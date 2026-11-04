package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"streaks/internal/streak"
	"streaks/internal/syncer"
)

func TestValidateCheckInDay(t *testing.T) {
	today, _ := streak.ParseDay("2026-11-04")
	tests := []struct {
		offset  streak.Day
		wantErr bool
	}{
		{1, true}, {0, false}, {-1, false}, {-2, false}, {-3, true},
	}
	for _, tc := range tests {
		if err := validateCheckInDay(today+tc.offset, today); (err != nil) != tc.wantErr {
			t.Errorf("offset %d: err = %v, wantErr %v", tc.offset, err, tc.wantErr)
		}
	}
}

func TestSessionRequired(t *testing.T) {
	h := (&Server{Now: time.Now, WebOrigin: "http://web"}).Handler()
	for _, path := range []string{"/habits", "/summary/today", "/auth/me", "/streaks/daily"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a session: %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthz: %d", rec.Code)
	}
}

func TestCrossOriginWritesRefused(t *testing.T) {
	h := (&Server{Now: time.Now, WebOrigin: "http://web"}).Handler()
	req := httptest.NewRequest(http.MethodPost, "/habits", nil)
	req.Header.Set("Origin", "http://evil")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST: %d", rec.Code)
	}
}

func TestDayState(t *testing.T) {
	today, _ := streak.ParseDay("2026-11-04") // A Wednesday.
	tracker := streak.NewTracker(streak.Habit{
		Schedules: []streak.Schedule{{From: today - 10, Mask: streak.MaskFromDays([]int{1, 2, 3})}},
		CheckIns:  []streak.Day{today - 2, today - 4},
	}, today)
	tests := map[streak.Day]string{
		today:     "pending",
		today - 1: "missed", // Tuesday.
		today - 2: "done",   // Monday.
		today - 3: "rest",   // Sunday.
		today - 4: "bonus",  // Saturday, checked in.
	}
	for d, want := range tests {
		if got := dayState(tracker, d, today); got != want {
			t.Errorf("%s = %s, want %s", d, got, want)
		}
	}
}

func TestTargetMaskIgnoresArchiveRows(t *testing.T) {
	mask := streak.MaskFromDays([]int{1, 3})
	got := targetMask([]streak.Schedule{{From: 10, Mask: mask}, {From: 20, Mask: 0}})
	if got != mask {
		t.Errorf("targetMask = %b, want %b", got, mask)
	}
}

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

	owned, _ := s.issueFor("7")
	if s.consumeFor(owned, "8") || s.consume(owned) {
		t.Error("state accepted for the wrong owner")
	}
	if !s.consumeFor(owned, "7") {
		t.Error("state rejected for its owner")
	}

	expired, _ := s.issue()
	now = now.Add(stateLifetime + time.Second)
	if s.consume(expired) {
		t.Error("expired state accepted")
	}
}

func TestCallbackRejectsMissingState(t *testing.T) {
	srv := &Server{Sync: &syncer.Syncer{}, Now: time.Now}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oauth/google/callback?state=nope&code=c", nil))
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "error=expired") {
		t.Errorf("code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}
