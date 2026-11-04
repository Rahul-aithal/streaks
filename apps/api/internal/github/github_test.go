package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchActivityFiltersEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		if r.URL.Path != "/users/me/events" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `[
			{"id":"1","type":"PushEvent","created_at":"2026-11-03T18:30:00Z","payload":{"ref":"refs/heads/x"}},
			{"id":"2","type":"PullRequestEvent","created_at":"2026-11-03T10:00:00Z","payload":{"action":"opened"}},
			{"id":"3","type":"PullRequestEvent","created_at":"2026-11-03T10:00:00Z","payload":{"action":"closed"}},
			{"id":"4","type":"WatchEvent","created_at":"2026-11-03T10:00:00Z","payload":{"action":"started"}}
		]`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	got, err := c.FetchActivity(context.Background(), "me")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "2" {
		t.Errorf("got %+v, want events 1 and 2", got)
	}
}

func TestFetchActivityPagination(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		if page == "2" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		events := make([]map[string]any, perPage)
		for i := range events {
			events[i] = map[string]any{"id": fmt.Sprint(i), "type": "PushEvent", "created_at": "2026-11-03T10:00:00Z"}
		}
		_ = json.NewEncoder(w).Encode(events)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	got, err := c.FetchActivity(context.Background(), "me")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != perPage || strings.Join(pages, ",") != "1,2" {
		t.Errorf("events=%d pages=%v", len(got), pages)
	}
}

func TestFetchActivityReportsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad credentials", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	if _, err := c.FetchActivity(context.Background(), "me"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v, want a 401 error", err)
	}
}

func TestLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"login":"octocat"}`)
	}))
	defer srv.Close()

	if got, err := NewClient(srv.URL, "good").Login(context.Background()); err != nil || got != "octocat" {
		t.Errorf("Login = %q, %v", got, err)
	}
	if _, err := NewClient(srv.URL, "bad").Login(context.Background()); !errors.Is(err, ErrBadToken) {
		t.Errorf("err = %v, want ErrBadToken", err)
	}
}
