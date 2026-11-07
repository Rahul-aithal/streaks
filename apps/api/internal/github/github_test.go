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
	"time"
)

func TestContributions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		var req struct {
			Query     string
			Variables map[string]string
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Variables["login"] != "me" || !strings.Contains(req.Query, "contributionCalendar") {
			t.Errorf("request = %+v", req)
		}
		from, _ := time.Parse(time.RFC3339, req.Variables["from"])
		to, _ := time.Parse(time.RFC3339, req.Variables["to"])
		if span := to.Sub(from); span > 365*24*time.Hour {
			t.Errorf("range %v is longer than GitHub allows", span)
		}
		fmt.Fprint(w, `{"data":{"user":{"contributionsCollection":{"contributionCalendar":{"weeks":[
			{"contributionDays":[{"date":"2026-11-01","contributionCount":35},{"date":"2026-11-02","contributionCount":0}]},
			{"contributionDays":[{"date":"2026-11-07","contributionCount":1}]}]}}}}}`)
	}))
	defer srv.Close()

	now := time.Date(2026, 11, 7, 13, 0, 0, 0, time.UTC)
	days, from, to, err := NewClient(srv.URL, "tok").Contributions(context.Background(), "me", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 || days[0].Day.String() != "2026-11-01" || days[0].Count != 35 || days[1].Count != 1 {
		t.Errorf("days = %+v (days without contributions should be left out)", days)
	}
	if to.String() != "2026-11-07" || from.String() != "2025-11-08" {
		t.Errorf("range = %s..%s", from, to)
	}
}

func TestContributionsErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"rejected token", http.StatusUnauthorized, `{"message":"Bad credentials"}`, ErrBadToken.Error()},
		{"graphql error", http.StatusOK, `{"errors":[{"message":"rate limited"}]}`, "rate limited"},
		{"unknown user", http.StatusOK, `{"data":{"user":null}}`, "not found"},
		{"server error", http.StatusBadGateway, `oops`, "502"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()
			_, _, _, err := NewClient(srv.URL, "tok").Contributions(context.Background(), "me", time.Now())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
			if tt.status == http.StatusUnauthorized && !errors.Is(err, ErrBadToken) {
				t.Error("a 401 should be ErrBadToken")
			}
		})
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
