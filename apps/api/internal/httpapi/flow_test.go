package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"streaks/internal/github"
	"streaks/internal/gtasks"
	"streaks/internal/mailer"
	"streaks/internal/reminder"
	"streaks/internal/secretbox"
	"streaks/internal/store"
	"streaks/internal/syncer"
)

// fakeServices plays Google and GitHub. Who you are comes from the code or token.
type fakeServices struct {
	mu        sync.Mutex
	revoked   map[string]bool
	dead      map[string]bool // Google refresh tokens now rejected.
	noRefresh map[string]bool // Codes that return no refresh token, like a returning user.
	noScope   map[string]bool // Codes where the user unticked Tasks.
	ghIssued  int             // GitHub tokens handed out.
	ghDead    map[string]bool // GitHub tokens the events API now refuses.
}

func (f *fakeServices) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Form.Get("grant_type") == "authorization_code" {
			code := r.Form.Get("code")
			who := strings.Split(strings.TrimPrefix(code, "code-"), "-")[0]
			claims := fmt.Sprintf(`{"sub":"sub-%s","email":"%s@Example.com","email_verified":true,"name":"%s","picture":"http://img/%s"}`, who, who, who, who)
			scope := "openid email profile " + gtasks.TasksScope
			if f.noScope[code] {
				scope = "openid email profile"
			}
			resp := map[string]string{
				"scope":    scope,
				"id_token": "h." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".s",
			}
			if !f.noRefresh[code] {
				resp["refresh_token"] = "rt-" + who
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		token := r.Form.Get("refresh_token")
		if f.dead[token] {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_grant"}`)
			return
		}
		fmt.Fprintf(w, `{"access_token":"at-%s"}`, strings.TrimPrefix(token, "rt-"))
	})
	mux.HandleFunc("POST /revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		f.revoked[r.Form.Get("token")] = true
	})
	mux.HandleFunc("GET /users/@me/lists", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"items":[{"id":"L"}]}`)
	})
	mux.HandleFunc("GET /lists/L/tasks", func(w http.ResponseWriter, r *http.Request) {
		who := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer at-")
		// All users deliberately share a task ID.
		fmt.Fprintf(w, `{"items":[{"id":"t1","title":"%s task","status":"needsAction","due":"2026-11-04T00:00:00.000Z"}]}`, who)
	})
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		who, ok := strings.CutPrefix(r.Form.Get("code"), "ghcode-")
		if !ok || r.Form.Get("client_secret") != "gh-secret" {
			fmt.Fprint(w, `{"error":"bad_verification_code"}`) // GitHub really does send a 200.
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.ghIssued++
		fmt.Fprintf(w, `{"access_token":"gh-%s"}`, who)
	})
	mux.HandleFunc("DELETE /applications/gh-client/token", func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		if user != "gh-client" || pass != "gh-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			AccessToken string `json:"access_token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.revoked[body.AccessToken] = true
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !strings.HasPrefix(token, "gh-") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		login, _, _ := strings.Cut(strings.TrimPrefix(token, "gh-"), "~") // gh-<login>~<n> is a newer token.
		fmt.Fprintf(w, `{"login":"%s"}`, login)
	})
	mux.HandleFunc("GET /users/{login}/events", func(w http.ResponseWriter, r *http.Request) {
		login := r.PathValue("login")
		f.mu.Lock()
		dead := f.ghDead[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		f.mu.Unlock()
		if dead {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// All users deliberately share an event ID. 2026-11-04 09:00 IST.
		fmt.Fprintf(w, `[{"id":"e1","type":"PushEvent","created_at":"2026-11-04T03:30:00Z","payload":{}},
			{"id":"%s-2","type":"PushEvent","created_at":"2026-11-04T04:00:00Z","payload":{}}]`, login)
	})
	return httptest.NewServer(mux)
}

// browser is one browser, with its own cookies.
type browser struct {
	t      *testing.T
	api    string
	client *http.Client
}

func newBrowser(t *testing.T, api string) *browser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &browser{t: t, api: api, client: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (b *browser) call(method, path string, body, out any) int {
	b.t.Helper()
	var reader *bytes.Reader = bytes.NewReader(nil)
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			b.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, b.api+path, reader)
	if err != nil {
		b.t.Fatal(err)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			b.t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

// startLogin returns the state Google would be given, and the prompt requested.
func (b *browser) startLogin(query string) (state, prompt string) {
	b.t.Helper()
	resp, err := b.client.Get(b.api + "/auth/google/start" + query)
	if err != nil {
		b.t.Fatal(err)
	}
	resp.Body.Close()
	return statePrompt(b.t, resp.Header.Get("Location"))
}

func statePrompt(t *testing.T, location string) (string, string) {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state"), u.Query().Get("prompt")
}

func (b *browser) callback(query string) string {
	b.t.Helper()
	resp, err := b.client.Get(b.api + "/oauth/google/callback?" + query)
	if err != nil {
		b.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.Header.Get("Location")
}

// startGitHub returns the redirect GitHub would be sent, and its state.
func (b *browser) startGitHub() (location, state string) {
	b.t.Helper()
	resp, err := b.client.Get(b.api + "/auth/github/start")
	if err != nil {
		b.t.Fatal(err)
	}
	resp.Body.Close()
	location = resp.Header.Get("Location")
	state, _ = statePrompt(b.t, location)
	return location, state
}

func (b *browser) githubCallback(query string) string {
	b.t.Helper()
	resp, err := b.client.Get(b.api + "/oauth/github/callback?" + query)
	if err != nil {
		b.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.Header.Get("Location")
}

func (b *browser) login(code string) string {
	b.t.Helper()
	state, _ := b.startLogin("")
	return b.callback("state=" + state + "&code=" + code)
}

type meResponse struct {
	Accounts []struct {
		ID     int64
		Email  string
		Active bool
	}
	Google struct {
		NeedsReconnect bool `json:"needs_reconnect"`
	}
	GitHub struct {
		Accounts []struct {
			ID        int64
			Login     string
			LastError *string `json:"last_error"`
		}
	}
}

func (m meResponse) githubLogins() string {
	var logins []string
	for _, a := range m.GitHub.Accounts {
		logins = append(logins, a.Login)
	}
	return strings.Join(logins, ",")
}

func (b *browser) me() meResponse {
	b.t.Helper()
	var m meResponse
	if code := b.call(http.MethodGet, "/auth/me", nil, &m); code != http.StatusOK {
		b.t.Fatalf("/auth/me: %d", code)
	}
	return m
}

func (m meResponse) active() (id int64, email string) {
	for _, a := range m.Accounts {
		if a.Active {
			return a.ID, a.Email
		}
	}
	return 0, ""
}

func (b *browser) habitNames() []string {
	b.t.Helper()
	var habits []struct{ Name string }
	if code := b.call(http.MethodGet, "/habits", nil, &habits); code != http.StatusOK {
		b.t.Fatalf("/habits: %d", code)
	}
	names := []string{}
	for _, h := range habits {
		names = append(names, h.Name)
	}
	return names
}

type summary struct {
	Habits []struct {
		Name          string
		Status        string
		Contributions *int
		Recent        []struct{ State string }
	}
	Tasks struct {
		Total          int
		NeedsReconnect bool `json:"needs_reconnect"`
		Items          []struct{ Title string }
	}
	GitHub struct {
		Linked        bool
		Contributions int
	}
}

func (b *browser) summary() summary {
	b.t.Helper()
	var s summary
	if code := b.call(http.MethodGet, "/summary/today", nil, &s); code != http.StatusOK {
		b.t.Fatalf("/summary/today: %d", code)
	}
	return s
}

// Set TEST_DATABASE_URL to a disposable, migrated database to run this test. It
// deletes rows, so never point it at real data. Run with `go test -p 1`, because
// the store test uses the same database.
func TestLoginFlowAndIsolation(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DELETE FROM users`)
	exec(`DELETE FROM habits`)

	st, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	fake := &fakeServices{
		revoked: map[string]bool{}, dead: map[string]bool{}, ghDead: map[string]bool{},
		noRefresh: map[string]bool{"code-alice-return": true, "code-dave-first": true, "code-bob-return": true},
		noScope:   map[string]bool{"code-erin": true},
	}
	services := fake.server()
	defer services.Close()

	google := gtasks.NewClient("id", "secret", "http://web/api/oauth/google/callback")
	google.AuthURL, google.TokenURL, google.RevokeURL, google.BaseURL =
		services.URL+"/auth", services.URL+"/token", services.URL+"/revoke", services.URL
	box, err := secretbox.New(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 11, 4, 6, 0, 0, 0, time.UTC) }
	server := &Server{
		Store: st, Now: now, WebOrigin: "http://web",
		Sync: &syncer.Syncer{
			Store: st, Google: google, Box: box, GitHubBaseURL: services.URL, Interval: time.Hour, Now: now,
		},
	}
	api := httptest.NewServer(server.Handler())
	defer api.Close()

	// A habit left unowned by the single-user migration.
	var unowned int64
	if err := conn.QueryRow(ctx, `INSERT INTO habits (name) VALUES ('old habit') RETURNING id`).Scan(&unowned); err != nil {
		t.Fatal(err)
	}

	b := newBrowser(t, api.URL)
	if code := b.call(http.MethodGet, "/habits", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("habits before login: %d", code)
	}

	// --- Sign-in checks ---
	state, _ := b.startLogin("")
	if loc := newBrowser(t, api.URL).callback("state=" + state + "&code=code-alice"); !strings.Contains(loc, "error=expired") {
		t.Errorf("state from another browser accepted: %q", loc)
	}
	state, _ = b.startLogin("")
	if loc := b.callback("state=" + state + "&error=access_denied"); !strings.Contains(loc, "error=denied") {
		t.Errorf("denied: %q", loc)
	}
	if loc := b.login("code-erin"); !strings.Contains(loc, "error=missing_scope") {
		t.Errorf("unticked Tasks: %q", loc)
	}

	// A brand-new account with no refresh token is sent back for consent.
	state, _ = b.startLogin("")
	resp, err := b.client.Get(api.URL + "/oauth/google/callback?state=" + state + "&code=code-dave-first")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	consentState, prompt := statePrompt(t, resp.Header.Get("Location"))
	if prompt != "select_account consent" || consentState == "" {
		t.Errorf("new account without token: location %q", resp.Header.Get("Location"))
	}

	// --- Alice signs in, and claims the unowned habit ---
	state, _ = b.startLogin("")
	if loc := b.callback("state=" + state + "&code=code-alice"); loc != "/" {
		t.Fatalf("alice login: %q", loc)
	}
	if loc := b.callback("state=" + state + "&code=code-alice"); !strings.Contains(loc, "error=expired") {
		t.Errorf("state replay: %q", loc)
	}
	if got := b.habitNames(); !slices.Equal(got, []string{"old habit"}) {
		t.Errorf("alice's habits = %v", got)
	}

	var aliceHabit struct{ ID int64 }
	if code := b.call(http.MethodPost, "/habits", map[string]any{"name": "alice reads"}, &aliceHabit); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	if code := b.call(http.MethodPut, fmt.Sprintf("/habits/%d/check-ins/2026-11-04", aliceHabit.ID), nil, nil); code != http.StatusOK {
		t.Fatalf("check in: %d", code)
	}
	if s := b.summary(); s.Tasks.Total != 1 || s.Tasks.Items[0].Title != "alice task" {
		t.Errorf("alice's tasks = %+v", s.Tasks)
	}
	aliceID, _ := b.me().active()

	// --- Bob joins the same browser and becomes active ---
	if loc := b.login("code-bob"); loc != "/" {
		t.Fatalf("bob login: %q", loc)
	}
	m := b.me()
	bobID, email := m.active()
	if len(m.Accounts) != 2 || email != "bob@Example.com" {
		t.Fatalf("after adding bob: %+v", m)
	}

	// --- Bob can't see or touch anything of Alice's ---
	if got := b.habitNames(); len(got) != 0 {
		t.Errorf("bob sees alice's habits: %v", got)
	}
	if s := b.summary(); s.Tasks.Total != 1 || s.Tasks.Items[0].Title != "bob task" || len(s.Habits) != 0 {
		t.Errorf("bob's summary = %+v", s)
	}
	path := fmt.Sprintf("/habits/%d", aliceHabit.ID)
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, path, nil},
		{http.MethodPatch, path, map[string]any{"name": "hacked"}},
		{http.MethodDelete, path, nil},
		{http.MethodPost, path + "/archive", nil},
		{http.MethodPost, path + "/unarchive", nil},
		{http.MethodGet, path + "/history", nil},
		{http.MethodPut, path + "/check-ins/2026-11-03", nil},
		{http.MethodDelete, path + "/check-ins/2026-11-04", nil},
	} {
		if code := b.call(c.method, c.path, c.body, nil); code != http.StatusNotFound {
			t.Errorf("bob %s %s: %d, want 404", c.method, c.path, code)
		}
	}
	var checkIns int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM check_ins WHERE habit_id = $1`, aliceHabit.ID).Scan(&checkIns); err != nil {
		t.Fatal(err)
	}
	if checkIns != 1 {
		t.Errorf("alice's check-ins changed: %d", checkIns)
	}

	// --- Swapping needs no new sign-in, and only to accounts in this browser ---
	if code := b.call(http.MethodPost, "/auth/switch", map[string]any{"user_id": aliceID}, nil); code != http.StatusNoContent {
		t.Fatalf("switch: %d", code)
	}
	if id, _ := b.me().active(); id != aliceID {
		t.Error("switch didn't activate alice")
	}
	if got := b.habitNames(); !slices.Equal(got, []string{"old habit", "alice reads"}) {
		t.Errorf("alice's habits after switch = %v", got)
	}
	other := newBrowser(t, api.URL)
	other.login("code-alice")
	if code := other.call(http.MethodPost, "/auth/switch", map[string]any{"user_id": bobID}, nil); code != http.StatusForbidden {
		t.Errorf("switching to an account not signed in here: %d", code)
	}

	// --- GitHub is per user ---
	if code := b.call(http.MethodPost, "/sync/github", nil, nil); code != http.StatusConflict {
		t.Errorf("sync before linking: %d", code)
	}
	if code := b.call(http.MethodPut, "/settings/github", map[string]any{"token": "wrong"}, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("bad GitHub token: %d", code)
	}
	var gh struct{ ID int64 }
	if code := b.call(http.MethodPost, "/habits", map[string]any{"name": "code", "source": "github"}, &gh); code != http.StatusCreated {
		t.Fatalf("github habit: %d", code)
	}
	for range 2 { // Linking again must not double-count.
		if code := b.call(http.MethodPut, "/settings/github", map[string]any{"token": "gh-alicehub"}, nil); code != http.StatusOK {
			t.Fatalf("link github: %d", code)
		}
	}
	if got := b.me().githubLogins(); got != "alicehub" {
		t.Errorf("github accounts = %q", got)
	}
	s := b.summary()
	if s.GitHub.Contributions != 2 {
		t.Errorf("alice's contributions = %d", s.GitHub.Contributions)
	}
	for _, h := range s.Habits {
		if h.Name == "code" && h.Status != "done" {
			t.Errorf("github habit status = %q", h.Status)
		}
		if len(h.Recent) != 7 {
			t.Errorf("recent days = %d", len(h.Recent))
		}
	}
	if code := b.call(http.MethodPut, fmt.Sprintf("/habits/%d/check-ins/2026-11-04", gh.ID), nil, nil); code != http.StatusConflict {
		t.Errorf("manual check-in on a GitHub habit: %d", code)
	}

	// Bob linking the same event IDs must not collide, or see Alice's counts.
	b.call(http.MethodPost, "/auth/switch", map[string]any{"user_id": bobID}, nil)
	if s := b.summary(); s.GitHub.Linked || s.GitHub.Contributions != 0 {
		t.Errorf("bob sees alice's GitHub: %+v", s.GitHub)
	}
	b.call(http.MethodPut, "/settings/github", map[string]any{"token": "gh-bobhub"}, nil)
	if s := b.summary(); s.GitHub.Contributions != 2 {
		t.Errorf("bob's contributions = %d", s.GitHub.Contributions)
	}

	// --- Tokens are encrypted and never returned ---
	users, err := st.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if strings.Contains(u.RefreshTokenEnc, "rt-") {
			t.Errorf("google token in plaintext: %q", u.RefreshTokenEnc)
		}
	}
	rows, err := conn.Query(ctx, `SELECT token_enc FROM github_accounts`)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(sealed) != 2 {
		t.Fatalf("github tokens = %d, %v", len(sealed), err)
	}
	for _, token := range sealed {
		if strings.Contains(token, "gh-") {
			t.Errorf("github token in plaintext: %q", token)
		}
	}
	var raw json.RawMessage
	b.call(http.MethodGet, "/auth/me", nil, &raw)
	if strings.Contains(string(raw), "_enc") || strings.Contains(string(raw), "gh-") {
		t.Errorf("API exposes a token: %s", raw)
	}
	var hash string
	if err := conn.QueryRow(ctx, `SELECT token_hash FROM sessions LIMIT 1`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(api.URL)
	for _, c := range b.client.Jar.Cookies(u) {
		if c.Name == sessionCookie && c.Value == hash {
			t.Error("session token stored unhashed")
		}
	}

	// --- A dead Google token flags only that user, keeps tasks, and forces consent ---
	fake.mu.Lock()
	fake.dead["rt-bob"] = true
	fake.mu.Unlock()
	if code := b.call(http.MethodPost, "/sync/google-tasks", nil, nil); code != http.StatusBadGateway {
		t.Errorf("sync with a dead token: %d", code)
	}
	if s := b.summary(); !s.Tasks.NeedsReconnect || s.Tasks.Total != 1 {
		t.Errorf("after dead token: %+v", s.Tasks)
	}
	state, _ = b.startLogin("")
	resp, err = b.client.Get(api.URL + "/oauth/google/callback?state=" + state + "&code=code-bob-return")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, prompt := statePrompt(t, resp.Header.Get("Location")); prompt != "select_account consent" {
		t.Errorf("reconnect without a new token: %q", resp.Header.Get("Location"))
	}
	fake.mu.Lock()
	delete(fake.dead, "rt-bob")
	fake.mu.Unlock()
	state, prompt = b.startLogin("?consent=1")
	if prompt != "select_account consent" {
		t.Errorf("?consent=1 prompt = %q", prompt)
	}
	if loc := b.callback("state=" + state + "&code=code-bob"); loc != "/" {
		t.Errorf("reconnect: %q", loc)
	}
	if b.summary().Tasks.NeedsReconnect {
		t.Error("reconnecting should clear the flag")
	}

	// --- A returning user with no new token keeps the old one ---
	if loc := b.login("code-alice-return"); loc != "/" {
		t.Errorf("returning user: %q", loc)
	}
	if _, email := b.me().active(); email != "alice@Example.com" {
		t.Errorf("active after returning = %q", email)
	}
	if s := b.summary(); s.Tasks.NeedsReconnect || s.Tasks.Total != 1 {
		t.Errorf("returning user's token was lost: %+v", s.Tasks)
	}
	if len(b.me().Accounts) != 2 {
		t.Error("signing in again shouldn't duplicate an account")
	}

	// --- Signing out moves to the other account, then ends the session ---
	var out struct {
		SignedIn bool `json:"signed_in"`
	}
	b.call(http.MethodPost, "/auth/sign-out", nil, &out)
	if !out.SignedIn {
		t.Error("should still be signed in as bob")
	}
	if id, _ := b.me().active(); id != bobID {
		t.Error("sign-out should activate bob")
	}
	cookies := b.client.Jar.Cookies(u)

	// Deleting Bob removes his data and leaves Alice's alone.
	b.login("code-alice")
	b.call(http.MethodPost, "/auth/switch", map[string]any{"user_id": bobID}, nil)
	if code := b.call(http.MethodDelete, "/account", nil, &out); code != http.StatusOK || !out.SignedIn {
		t.Errorf("delete account: %d %+v", code, out)
	}
	if id, _ := b.me().active(); id != aliceID {
		t.Error("deleting bob should leave alice active")
	}
	fake.mu.Lock()
	revokedBob := fake.revoked["rt-bob"]
	fake.mu.Unlock()
	if !revokedBob {
		t.Error("deleting an account should revoke its token")
	}
	var bobRows int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM tasks WHERE user_id = $1)
		+ (SELECT count(*) FROM github_events WHERE user_id = $1)
		+ (SELECT count(*) FROM users WHERE id = $1)`, bobID).Scan(&bobRows); err != nil {
		t.Fatal(err)
	}
	if bobRows != 0 {
		t.Errorf("bob's rows left: %d", bobRows)
	}
	if got := b.habitNames(); len(got) != 3 {
		t.Errorf("alice's habits after deleting bob = %v", got)
	}

	// A sign-out invalidates the session on the server, not just the cookie.
	b.call(http.MethodPost, "/auth/sign-out", nil, &out)
	if out.SignedIn {
		t.Error("last sign-out should end the session")
	}
	replay := newBrowser(t, api.URL)
	replay.client.Jar.SetCookies(u, cookies)
	if code := replay.call(http.MethodGet, "/auth/me", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("old cookie still works: %d", code)
	}

	// Expired sessions stop working.
	if code := other.call(http.MethodGet, "/auth/me", nil, nil); code != http.StatusOK {
		t.Fatalf("other browser before expiry: %d", code)
	}
	exec(`UPDATE sessions SET expires_at = now() - interval '1 minute'`)
	if code := other.call(http.MethodGet, "/auth/me", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("expired session: %d", code)
	}

	// Sign out of everything.
	third := newBrowser(t, api.URL)
	third.login("code-alice")
	third.login("code-dave")
	if code := third.call(http.MethodPost, "/auth/sign-out-all", nil, nil); code != http.StatusNoContent {
		t.Errorf("sign out all: %d", code)
	}
	if code := third.call(http.MethodGet, "/auth/me", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("after sign out all: %d", code)
	}

	// Google decides who gets this far, so any verified account can sign in.
	carol := newBrowser(t, api.URL)
	if loc := carol.login("code-carol"); strings.Contains(loc, "error=") {
		t.Errorf("new account refused: %q", loc)
	}
	if code := carol.call(http.MethodGet, "/auth/me", nil, nil); code != http.StatusOK {
		t.Errorf("new account has no session: %d", code)
	}

	// --- Connect GitHub ---
	server.Sync.GitHubOAuth = &github.OAuth{
		ClientID: "gh-client", ClientSecret: "gh-secret", RedirectURI: "http://web/api/oauth/github/callback",
		WebURL: services.URL, APIURL: services.URL, HTTP: services.Client(),
	}
	loc, state := carol.startGitHub()
	if !strings.HasPrefix(loc, services.URL+"/login/oauth/authorize?") || !strings.Contains(loc, "client_id=gh-client") || state == "" {
		t.Errorf("authorize redirect: %q", loc)
	}
	if strings.Contains(loc, "scope=") {
		t.Errorf("should ask for no scopes: %q", loc)
	}
	if loc := carol.githubCallback("state=" + state + "&error=access_denied"); !strings.Contains(loc, "github=denied") {
		t.Errorf("declined: %q", loc)
	}
	_, state = carol.startGitHub()
	if loc := carol.githubCallback("state=" + state + "&code=nope"); !strings.Contains(loc, "github=failed") {
		t.Errorf("bad code: %q", loc)
	}
	_, state = carol.startGitHub()
	if loc := newBrowser(t, api.URL).githubCallback("state=" + state + "&code=ghcode-carolhub"); !strings.Contains(loc, "/login?error=expired") {
		t.Errorf("callback with no session: %q", loc)
	}
	_, state = carol.startGitHub()
	if loc := carol.githubCallback("state=" + state + "&code=ghcode-carolhub"); !strings.Contains(loc, "github=connected") {
		t.Errorf("connect: %q", loc)
	}
	if got := carol.me().githubLogins(); got != "carolhub" {
		t.Errorf("connected accounts = %q", got)
	}
	if loc := carol.githubCallback("state=" + state + "&code=ghcode-mallory"); !strings.Contains(loc, "github=expired") {
		t.Errorf("state replayed: %q", loc)
	}
	if carol.summary().GitHub.Contributions != 2 {
		t.Error("connecting should sync straight away")
	}

	// The account picker is requested, so a second GitHub account can be chosen.
	if loc, _ := carol.startGitHub(); !strings.Contains(loc, "prompt=select_account") {
		t.Errorf("no account picker: %q", loc)
	}
	// A second GitHub account is added, not swapped in, and activity is pooled.
	_, state = carol.startGitHub()
	if loc := carol.githubCallback("state=" + state + "&code=ghcode-carol2"); !strings.Contains(loc, "github=connected") {
		t.Errorf("second account: %q", loc)
	}
	if got := carol.me().githubLogins(); got != "carolhub,carol2" {
		t.Errorf("accounts after adding one = %q", got)
	}
	if got := carol.summary().GitHub.Contributions; got != 3 { // e1 is shared, plus one event each.
		t.Errorf("pooled contributions = %d", got)
	}
	fake.mu.Lock()
	revokedEarly := fake.revoked["gh-carolhub"]
	fake.mu.Unlock()
	if revokedEarly {
		t.Error("adding an account must not revoke another's token")
	}

	// Reconnecting an account already linked replaces its token and revokes the old one.
	_, state = carol.startGitHub()
	if loc := carol.githubCallback("state=" + state + "&code=ghcode-carolhub~2"); !strings.Contains(loc, "github=connected") {
		t.Errorf("reconnect: %q", loc)
	}
	if got := carol.me().githubLogins(); got != "carolhub,carol2" {
		t.Errorf("accounts after reconnecting = %q", got)
	}
	fake.mu.Lock()
	replaced := fake.revoked["gh-carolhub"]
	fake.mu.Unlock()
	if !replaced {
		t.Error("the replaced token should be revoked")
	}

	// One account failing doesn't stop the other's activity counting.
	fake.mu.Lock()
	fake.ghDead["gh-carol2"] = true
	fake.mu.Unlock()
	var syncErr struct{ Error string }
	if code := carol.call(http.MethodPost, "/sync/github", nil, &syncErr); code != http.StatusBadGateway || !strings.Contains(syncErr.Error, "carol2") {
		t.Errorf("partial failure: %d %q", code, syncErr.Error)
	}
	for _, a := range carol.me().GitHub.Accounts {
		if failed := a.LastError != nil; failed != (a.Login == "carol2") {
			t.Errorf("%s last error = %v", a.Login, a.LastError)
		}
	}
	if got := carol.summary().GitHub.Contributions; got != 3 {
		t.Errorf("contributions after a partial failure = %d", got)
	}
	fake.mu.Lock()
	fake.ghDead["gh-carol2"] = false
	fake.mu.Unlock()

	// A connection started by one account can't land on another: swapping accounts
	// mid-flow must not link GitHub to the wrong one.
	swap := newBrowser(t, api.URL)
	swap.login("code-erin")
	_, state = swap.startGitHub()
	swap.login("code-frank")
	if loc := swap.githubCallback("state=" + state + "&code=ghcode-erinhub"); !strings.Contains(loc, "github=expired") {
		t.Errorf("state for another account: %q", loc)
	}
	if got := swap.me().githubLogins(); got != "" {
		t.Errorf("frank got erin's GitHub: %q", got)
	}

	// Disconnecting one account revokes its token and leaves the other.
	var carol2 int64
	for _, a := range carol.me().GitHub.Accounts {
		if a.Login == "carol2" {
			carol2 = a.ID
		}
	}
	var aliceAccount int64
	if err := conn.QueryRow(ctx, `SELECT g.id FROM github_accounts g JOIN users u ON u.id = g.user_id
		WHERE u.google_sub = 'sub-alice'`).Scan(&aliceAccount); err != nil {
		t.Fatal(err)
	}
	if code := carol.call(http.MethodDelete, fmt.Sprintf("/settings/github/%d", aliceAccount), nil, nil); code != http.StatusNotFound {
		t.Errorf("disconnect another user's account: %d", code)
	}
	if code := carol.call(http.MethodDelete, fmt.Sprintf("/settings/github/%d", carol2), nil, nil); code != http.StatusNoContent {
		t.Errorf("disconnect: %d", code)
	}
	fake.mu.Lock()
	unlinked := fake.revoked["gh-carol2"]
	fake.mu.Unlock()
	if !unlinked {
		t.Error("disconnecting should revoke the token")
	}
	if got := carol.me().githubLogins(); got != "carolhub" {
		t.Errorf("accounts after disconnecting one = %q", got)
	}
	if code := carol.call(http.MethodDelete, fmt.Sprintf("/settings/github/%d", carol2), nil, nil); code != http.StatusNotFound {
		t.Errorf("disconnect twice: %d", code)
	}

	// Without an OAuth app, the button has nowhere to go and says so.
	server.Sync.GitHubOAuth = nil
	if loc, _ := carol.startGitHub(); !strings.Contains(loc, "github=failed") {
		t.Errorf("not configured: %q", loc)
	}

	// --- Email reminders ---
	var me struct {
		Email struct {
			Address    string
			Enabled    bool
			Configured bool
		}
	}
	carol.call(http.MethodGet, "/auth/me", nil, &me)
	if !me.Email.Enabled || me.Email.Configured || me.Email.Address != "carol@Example.com" {
		t.Errorf("email settings = %+v", me.Email)
	}
	if code := carol.call(http.MethodPost, "/settings/email/test", nil, nil); code != http.StatusConflict {
		t.Errorf("test email without SMTP: %d", code)
	}
	mail := &fakeMail{}
	server.Reminders = &reminder.Reminder{Store: st, Sync: server.Sync, Mail: mail, Now: now, AppURL: "http://web"}
	if code := carol.call(http.MethodPost, "/settings/email/test", nil, nil); code != http.StatusConflict {
		t.Errorf("test email with no habits: %d", code)
	}
	carol.call(http.MethodPost, "/habits", map[string]any{"name": "stretch", "source": "manual"}, nil)
	if code := carol.call(http.MethodPost, "/settings/email/test", nil, nil); code != http.StatusNoContent {
		t.Fatalf("test email: %d", code)
	}
	if len(mail.sent) != 1 || mail.sent[0].To != "carol@Example.com" || !strings.HasPrefix(mail.sent[0].Subject, "[Test] ") {
		t.Errorf("sent = %+v", mail.sent)
	}
	if !strings.Contains(mail.sent[0].Text, "stretch") || strings.Contains(mail.sent[0].Text, "code") {
		t.Errorf("test email should list only carol's habits:\n%s", mail.sent[0].Text)
	}
	if code := carol.call(http.MethodPut, "/settings/email", map[string]any{}, nil); code != http.StatusBadRequest {
		t.Errorf("missing enabled: %d", code)
	}
	if code := carol.call(http.MethodPut, "/settings/email", map[string]any{"enabled": false}, nil); code != http.StatusNoContent {
		t.Errorf("turn off: %d", code)
	}
	carol.call(http.MethodGet, "/auth/me", nil, &me)
	if me.Email.Enabled {
		t.Error("still on after turning off")
	}
	mail.fail = true
	if code := carol.call(http.MethodPost, "/settings/email/test", nil, nil); code != http.StatusBadGateway {
		t.Errorf("failed send: %d", code)
	}
}

type fakeMail struct {
	sent []mailer.Message
	fail bool
}

func (f *fakeMail) Send(_ context.Context, m mailer.Message) error {
	if f.fail {
		return errors.New("535 rejected")
	}
	f.sent = append(f.sent, m)
	return nil
}
