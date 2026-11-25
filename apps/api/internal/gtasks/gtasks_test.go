package gtasks

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"streaks/internal/store"
	"streaks/internal/streak"
)

func mustDay(t *testing.T, s string) *streak.Day {
	t.Helper()
	d, err := streak.ParseDay(s)
	if err != nil {
		t.Fatal(err)
	}
	return &d
}

func mustTime(t *testing.T, s string) *time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return &v
}

func TestForToday(t *testing.T) {
	today := *mustDay(t, "2026-11-04")
	tasks := []store.Task{
		{Title: "overdue", Status: "needsAction", Due: mustDay(t, "2026-11-03")},
		{Title: "due today", Status: "needsAction", Due: mustDay(t, "2026-11-04")},
		{Title: "due tomorrow", Status: "needsAction", Due: mustDay(t, "2026-11-05")},
		{Title: "undated open", Status: "needsAction"},
		{Title: "undated done today", Status: "completed", CompletedAt: mustTime(t, "2026-11-04T05:00:00Z")},
		{Title: "done today", Status: "completed", Due: mustDay(t, "2026-11-04"), CompletedAt: mustTime(t, "2026-11-04T05:00:00Z")},
		{Title: "done just after IST midnight", Status: "completed", Due: mustDay(t, "2026-11-01"), CompletedAt: mustTime(t, "2026-11-03T18:30:00Z")},
		{Title: "done just before IST midnight", Status: "completed", Due: mustDay(t, "2026-11-03"), CompletedAt: mustTime(t, "2026-11-03T18:29:59Z")},
	}
	items, snap := ForToday(tasks, today)

	want := store.TaskSnapshot{Total: 4, Completed: 2, Overdue: 1}
	if snap != want {
		t.Errorf("snapshot = %+v, want %+v", snap, want)
	}
	if len(items) != 4 {
		t.Errorf("items = %+v", items)
	}
}

func TestLater(t *testing.T) {
	today := *mustDay(t, "2026-11-04")
	tasks := []store.Task{
		{Title: "undated open", Status: "needsAction"},
		{Title: "next week", Status: "needsAction", Due: mustDay(t, "2026-11-11")},
		{Title: "due today", Status: "needsAction", Due: mustDay(t, "2026-11-04")},
		{Title: "tomorrow", Status: "needsAction", Due: mustDay(t, "2026-11-05")},
		{Title: "undated done today", Status: "completed", CompletedAt: mustTime(t, "2026-11-04T05:00:00Z")},
		{Title: "undated done yesterday", Status: "completed", CompletedAt: mustTime(t, "2026-11-03T05:00:00Z")},
		{Title: "future done", Status: "completed", Due: mustDay(t, "2026-11-09"), CompletedAt: mustTime(t, "2026-11-04T05:00:00Z")},
	}
	var got []string
	for _, task := range Later(tasks, today) {
		got = append(got, task.Title)
	}
	want := []string{"tomorrow", "next week", "undated open", "undated done today"}
	if !slices.Equal(got, want) {
		t.Errorf("Later = %v, want %v", got, want)
	}
}

func TestConvert(t *testing.T) {
	today := *mustDay(t, "2026-11-04")

	got, keep := convert(rawTask{ID: "a", Title: "x", Status: "needsAction", Due: "2026-11-04T00:00:00.000Z"}, "l", today)
	if !keep || got.Due == nil || got.Due.String() != "2026-11-04" {
		t.Errorf("due date should be taken as written: %+v", got)
	}

	_, keep = convert(rawTask{ID: "b", Status: "completed", Completed: mustTime(t, "2026-10-01T00:00:00Z")}, "l", today)
	if keep {
		t.Error("old completed tasks should be dropped")
	}
}

func TestFetchTasksRequestsHiddenAndPages(t *testing.T) {
	var taskQueries []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "rt" {
			t.Errorf("bad token form: %v", r.Form)
		}
		fmt.Fprint(w, `{"access_token":"at"}`)
	})
	mux.HandleFunc("GET /users/@me/lists", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		fmt.Fprint(w, `{"items":[{"id":"L1"}]}`)
	})
	mux.HandleFunc("GET /lists/L1/tasks", func(w http.ResponseWriter, r *http.Request) {
		taskQueries = append(taskQueries, r.URL.RawQuery)
		if r.URL.Query().Get("pageToken") == "" {
			fmt.Fprint(w, `{"items":[{"id":"t1","title":"one","status":"needsAction"}],"nextPageToken":"p2"}`)
			return
		}
		fmt.Fprint(w, `{"items":[{"id":"t2","title":"two","status":"needsAction"}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient("id", "secret", "http://cb")
	c.BaseURL, c.TokenURL = srv.URL, srv.URL+"/token"
	got, _, err := c.FetchTasks(context.Background(), "rt", *mustDay(t, "2026-11-04"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("tasks = %+v", got)
	}
	if len(taskQueries) != 2 || !strings.Contains(taskQueries[0], "showHidden=true") {
		t.Errorf("queries = %v", taskQueries)
	}
}

func TestExpiredRefreshTokenAsksToReconnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)
	}))
	defer srv.Close()

	c := NewClient("id", "topsecret", "http://cb")
	c.TokenURL = srv.URL
	_, _, err := c.FetchTasks(context.Background(), "rt", 0)
	if !errors.Is(err, ErrReconnect) {
		t.Errorf("err = %v, want ErrReconnect", err)
	}
}

func TestRefreshTokenFromAnotherClientAsksToReconnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"unauthorized_client","error_description":"Unauthorized"}`)
	}))
	defer srv.Close()

	c := NewClient("new-id", "topsecret", "http://cb")
	c.TokenURL = srv.URL
	if _, _, err := c.FetchTasks(context.Background(), "rt", 0); !errors.Is(err, ErrReconnect) {
		t.Errorf("err = %v, want ErrReconnect", err)
	}
}

func idToken(claims string) string {
	return "h." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".s"
}

func TestAuthCodeURL(t *testing.T) {
	c := NewClient("id", "secret", "http://localhost:5173/api/oauth/google/callback")
	u, err := url.Parse(c.AuthCodeURL("st", false))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("state") != "st" || q.Get("access_type") != "offline" || q.Get("prompt") != "select_account" ||
		q.Get("redirect_uri") != c.RedirectURI || !strings.Contains(q.Get("scope"), TasksScope) {
		t.Errorf("query = %v", q)
	}
	if strings.Contains(u.String(), "secret") {
		t.Error("URL leaks the client secret")
	}
	forced, _ := url.Parse(c.AuthCodeURL("st", true))
	if forced.Query().Get("prompt") != "select_account consent" {
		t.Errorf("forced prompt = %q", forced.Query().Get("prompt"))
	}
}

func TestExchangeCode(t *testing.T) {
	good := idToken(`{"sub":"123","email":"a@example.com","email_verified":true,"name":"A","picture":"http://p"}`)
	tests := []struct {
		name, response string
		wantErr        error
		wantOtherErr   bool
		wantRefresh    string
	}{
		{name: "new user", response: `{"refresh_token":"rt","scope":"openid ` + TasksScope + `","id_token":"` + good + `"}`, wantRefresh: "rt"},
		{name: "returning user has no refresh token", response: `{"scope":"` + TasksScope + `","id_token":"` + good + `"}`},
		{name: "tasks unticked", response: `{"refresh_token":"rt","scope":"openid email","id_token":"` + good + `"}`, wantErr: ErrMissingScope},
		{name: "unverified email", response: `{"scope":"` + TasksScope + `","id_token":"` + idToken(`{"sub":"1","email":"a@example.com","email_verified":false}`) + `"}`, wantErr: ErrUnverifiedEmail},
		{name: "bad ID token", response: `{"scope":"` + TasksScope + `","id_token":"nope"}`, wantOtherErr: true},
		{name: "no sub", response: `{"scope":"` + TasksScope + `","id_token":"` + idToken(`{"email":"a"}`) + `"}`, wantOtherErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.response)
			}))
			defer srv.Close()
			c := NewClient("id", "secret", "http://cb")
			c.TokenURL = srv.URL
			got, err := c.ExchangeCode(context.Background(), "code")
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("err = %v, want %v", err, tc.wantErr)
				}
			case tc.wantOtherErr:
				if err == nil {
					t.Error("want an error")
				}
			default:
				if err != nil || got.Sub != "123" || got.Email != "a@example.com" || got.Name != "A" ||
					got.AvatarURL != "http://p" || got.RefreshToken != tc.wantRefresh {
					t.Errorf("got %+v, %v", got, err)
				}
			}
		})
	}
}

func TestSetTaskStatus(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	status := http.StatusOK
	reply := ""
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"access_token":"at"}`)
	})
	mux.HandleFunc("/lists/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotBody = r.Method, r.URL.EscapedPath(), string(body)
		w.WriteHeader(status)
		fmt.Fprint(w, reply)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient("id", "secret", "http://cb")
	c.TokenURL, c.BaseURL = srv.URL+"/token", srv.URL

	reply = `{"id":"t/1","title":"Write","status":"completed","due":"2026-11-04T00:00:00.000Z","completed":"2026-11-04T05:00:00Z"}`
	got, err := c.SetTaskStatus(context.Background(), "rt", "L 1", "t/1", true)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/lists/L%201/tasks/t%2F1" || gotBody != `{"status":"completed"}` {
		t.Errorf("request = %s %s %s", gotMethod, gotPath, gotBody)
	}
	if got.Status != "completed" || got.CompletedAt == nil || got.ListID != "L 1" || got.Due == nil || got.Due.String() != "2026-11-04" {
		t.Errorf("task = %+v", got)
	}

	reply = `{"id":"t/1","title":"Write","status":"needsAction"}`
	if _, err := c.SetTaskStatus(context.Background(), "rt", "L 1", "t/1", false); err != nil || !strings.Contains(gotBody, `"completed":null`) {
		t.Errorf("reopen: body %s, err %v", gotBody, err)
	}

	status, reply = http.StatusForbidden, `{"error":{"code":403,"message":"Request had insufficient authentication scopes.","details":[{"reason":"ACCESS_TOKEN_SCOPE_INSUFFICIENT"}]}}`
	if _, err := c.SetTaskStatus(context.Background(), "rt", "L", "t", true); !errors.Is(err, ErrNeedsWriteAccess) {
		t.Errorf("read-only token: %v", err)
	}
	status, reply = http.StatusNotFound, `{"error":{"code":404}}`
	if _, err := c.SetTaskStatus(context.Background(), "rt", "L", "t", true); !errors.Is(err, ErrTaskGone) {
		t.Errorf("deleted task: %v", err)
	}
}

func TestFetchTasksReportsWriteAccess(t *testing.T) {
	scope := ""
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"access_token":"at","scope":%q}`, scope)
	})
	mux.HandleFunc("GET /users/@me/lists", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"items":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient("id", "secret", "http://cb")
	c.TokenURL, c.BaseURL = srv.URL+"/token", srv.URL

	for _, tc := range []struct {
		scope    string
		writable bool
	}{
		{ReadOnlyScope, false},
		{TasksScope + " openid", true},
		{"", true}, // Unknown: don't nag.
	} {
		scope = tc.scope
		_, writable, err := c.FetchTasks(context.Background(), "rt", 0)
		if err != nil || writable != tc.writable {
			t.Errorf("scope %q: writable=%v err=%v, want %v", tc.scope, writable, err, tc.writable)
		}
	}
}

func TestReadOnlySignInIsAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"refresh_token":"rt","scope":"openid %s","id_token":%q}`, ReadOnlyScope,
			idToken(`{"sub":"1","email":"a@example.com","email_verified":true}`))
	}))
	defer srv.Close()
	c := NewClient("id", "secret", "http://cb")
	c.TokenURL = srv.URL
	if _, err := c.ExchangeCode(context.Background(), "code"); err != nil {
		t.Errorf("read-only sign-in: %v", err)
	}
}
