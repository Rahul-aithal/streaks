package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"streaks/internal/gtasks"
	"streaks/internal/secretbox"
	"streaks/internal/store"
	"streaks/internal/syncer"
)

func TestStateStore(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	s := newStateStore(func() time.Time { return now })

	state, err := s.issue()
	if err != nil {
		t.Fatal(err)
	}
	if s.consume("unknown") || s.consume("") {
		t.Error("unknown state accepted")
	}
	if !s.consume(state) {
		t.Error("valid state rejected")
	}
	if s.consume(state) {
		t.Error("state accepted twice")
	}

	expired, _ := s.issue()
	now = now.Add(stateLifetime + time.Second)
	if s.consume(expired) {
		t.Error("expired state accepted")
	}
}

func TestCallbackRejectsBadStateWithoutSecret(t *testing.T) {
	srv := &Server{Secret: "x", Sync: &syncer.Syncer{}, Now: time.Now}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oauth/google/callback?state=nope&code=c", nil))
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "google_error=expired") {
		t.Errorf("code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

// fakeGoogle plays Google's OAuth and Tasks endpoints for two accounts.
type fakeGoogle struct {
	mu      sync.Mutex
	revoked map[string]bool
	dead    map[string]bool // Refresh tokens Google now rejects.
	scope   string
}

func (f *fakeGoogle) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Form.Get("grant_type") == "authorization_code" {
			who := strings.TrimPrefix(r.Form.Get("code"), "code-")
			claims := fmt.Sprintf(`{"sub":"sub-%s","email":"%s@example.com"}`, who, who)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"refresh_token": "rt-" + who,
				"scope":         f.scope,
				"id_token":      "h." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".s",
			})
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
		// Both accounts deliberately use the same task ID.
		fmt.Fprintf(w, `{"items":[
			{"id":"t1","title":"%s due today","status":"needsAction","due":"2026-11-04T00:00:00.000Z"},
			{"id":"t2","title":"%s overdue","status":"needsAction","due":"2026-11-01T00:00:00.000Z"},
			{"id":"t3","title":"%s undated","status":"needsAction"}
		]}`, who, who, who)
	})
	return httptest.NewServer(mux)
}

type summaryResponse struct {
	Tasks struct {
		Total, Overdue int
		Items          []struct{ Account, Title string }
		Accounts       []struct {
			Email          string
			NeedsReconnect bool `json:"needs_reconnect"`
		}
	}
}

// Set TEST_DATABASE_URL to a disposable, migrated database to run this test. It
// deletes rows, so never point it at real data. Run with `go test -p 1`, because
// the store test uses the same database.
func TestGoogleMultiAccountFlow(t *testing.T) {
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
	if _, err := conn.Exec(ctx, `DELETE FROM google_accounts; DELETE FROM task_snapshots; DELETE FROM sync_runs`); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	fake := &fakeGoogle{revoked: map[string]bool{}, dead: map[string]bool{}, scope: "openid email " + gtasks.TasksScope}
	google := fake.server()
	defer google.Close()

	client := gtasks.NewClient("id", "secret", "http://localhost:5173/api/oauth/google/callback")
	client.TokenURL, client.RevokeURL, client.BaseURL = google.URL+"/token", google.URL+"/revoke", google.URL
	box, err := secretbox.New(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 11, 4, 6, 0, 0, 0, time.UTC) }
	server := &Server{
		Store: st, Secret: "s3cret", Now: now,
		Sync: &syncer.Syncer{Store: st, Google: client, Box: box, Interval: time.Hour, Now: now},
	}
	api := httptest.NewServer(server.Handler())
	defer api.Close()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	call := func(method, path string, out any) int {
		t.Helper()
		req, _ := http.NewRequest(method, api.URL+path, nil)
		req.Header.Set(secretHeader, "s3cret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode
	}
	summary := func() summaryResponse {
		t.Helper()
		var s summaryResponse
		call(http.MethodGet, "/summary/today", &s)
		return s
	}
	startLogin := func() string {
		t.Helper()
		var res struct{ URL string }
		call(http.MethodPost, "/google-accounts/connect", &res)
		u, err := url.Parse(res.URL)
		if err != nil {
			t.Fatal(err)
		}
		return u.Query().Get("state")
	}
	// Called without the secret, as Google's redirect is.
	callback := func(query string) string {
		t.Helper()
		resp, err := noRedirect.Get(api.URL + "/oauth/google/callback?" + query)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("Location")
	}

	// Connect routes need the secret.
	resp, err := http.Post(api.URL+"/google-accounts/connect", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("connect without secret: %d", resp.StatusCode)
	}

	// Connect two accounts. Each sync combines everything.
	state := startLogin()
	if loc := callback("state=" + state + "&code=code-alice"); loc != "/accounts?connected=1" {
		t.Fatalf("first connect: %q", loc)
	}
	if loc := callback("state=" + state + "&code=code-alice"); !strings.Contains(loc, "google_error=expired") {
		t.Errorf("state replay: %q", loc)
	}
	if loc := callback("state=" + startLogin() + "&code=code-bob"); loc != "/accounts?connected=1" {
		t.Fatalf("second connect: %q", loc)
	}

	s := summary()
	if s.Tasks.Total != 4 || s.Tasks.Overdue != 2 || len(s.Tasks.Accounts) != 2 {
		t.Fatalf("combined summary = %+v", s.Tasks)
	}
	emails := map[string]int{}
	for _, item := range s.Tasks.Items {
		emails[item.Account]++
	}
	if emails["alice@example.com"] != 2 || emails["bob@example.com"] != 2 {
		t.Errorf("task labels = %v", emails)
	}

	// Connecting the same account again updates it rather than duplicating.
	callback("state=" + startLogin() + "&code=code-alice")
	if s := summary(); len(s.Tasks.Accounts) != 2 {
		t.Errorf("reconnect duplicated the account: %+v", s.Tasks.Accounts)
	}

	// Tokens are encrypted at rest and never returned by the API.
	accounts, err := st.ListGoogleAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range accounts {
		if strings.Contains(a.RefreshTokenEnc, "rt-") {
			t.Errorf("token stored in plaintext: %q", a.RefreshTokenEnc)
		}
	}
	var raw json.RawMessage
	call(http.MethodGet, "/google-accounts", &raw)
	if strings.Contains(string(raw), "token_enc") || strings.Contains(string(raw), accounts[0].RefreshTokenEnc) {
		t.Errorf("API exposes the token: %s", raw)
	}

	// One account dying doesn't stop the other. Its cached tasks stay.
	fake.mu.Lock()
	fake.dead["rt-bob"] = true
	fake.mu.Unlock()
	if code := call(http.MethodPost, "/sync/google-tasks", nil); code != http.StatusBadGateway {
		t.Errorf("partial failure: %d", code)
	}
	s = summary()
	if s.Tasks.Total != 4 {
		t.Errorf("cached tasks should survive a failed sync: %+v", s.Tasks)
	}
	needs := map[string]bool{}
	for _, a := range s.Tasks.Accounts {
		needs[a.Email] = a.NeedsReconnect
	}
	if !needs["bob@example.com"] || needs["alice@example.com"] {
		t.Errorf("needs_reconnect = %v", needs)
	}

	// Reconnecting clears the flag.
	fake.mu.Lock()
	delete(fake.dead, "rt-bob")
	fake.mu.Unlock()
	callback("state=" + startLogin() + "&code=code-bob")
	for _, a := range summary().Tasks.Accounts {
		if a.NeedsReconnect {
			t.Errorf("%s still flagged", a.Email)
		}
	}

	// Unticking Tasks on the consent screen is refused.
	fake.mu.Lock()
	fake.scope = "openid email"
	fake.mu.Unlock()
	if loc := callback("state=" + startLogin() + "&code=code-carol"); !strings.Contains(loc, "google_error=missing_scope") {
		t.Errorf("missing scope: %q", loc)
	}
	if loc := callback("state=" + startLogin() + "&error=access_denied"); !strings.Contains(loc, "google_error=denied") {
		t.Errorf("denied: %q", loc)
	}

	// Disconnecting revokes, removes only that account's tasks, and fixes the snapshot.
	var alice int64
	accounts, _ = st.ListGoogleAccounts(ctx)
	for _, a := range accounts {
		if a.Email == "alice@example.com" {
			alice = a.ID
		}
	}
	if code := call(http.MethodDelete, fmt.Sprintf("/google-accounts/%d", alice), nil); code != http.StatusNoContent {
		t.Errorf("disconnect: %d", code)
	}
	fake.mu.Lock()
	revoked := fake.revoked["rt-alice"]
	fake.mu.Unlock()
	if !revoked {
		t.Error("token was not revoked")
	}
	s = summary()
	if s.Tasks.Total != 2 || len(s.Tasks.Accounts) != 1 {
		t.Errorf("after disconnect: %+v", s.Tasks)
	}
	var total int
	if err := conn.QueryRow(ctx, `SELECT total FROM task_snapshots WHERE day = '2026-11-04'`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("snapshot total = %d, want 2", total)
	}
	if code := call(http.MethodDelete, fmt.Sprintf("/google-accounts/%d", alice), nil); code != http.StatusNotFound {
		t.Errorf("disconnect twice: %d", code)
	}
}
