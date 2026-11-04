package gtasks

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	c := NewClient("id", "secret", "rt")
	c.BaseURL, c.TokenURL = srv.URL, srv.URL+"/token"
	got, err := c.FetchTasks(context.Background(), *mustDay(t, "2026-11-04"))
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

func TestExpiredRefreshTokenExplainsFix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)
	}))
	defer srv.Close()

	c := NewClient("id", "topsecret", "rt")
	c.TokenURL = srv.URL
	_, err := c.FetchTasks(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "make google-auth") {
		t.Errorf("err = %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "topsecret") {
		t.Errorf("error leaks the client secret: %v", err)
	}
}
