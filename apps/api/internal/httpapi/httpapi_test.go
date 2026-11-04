package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"streaks/internal/streak"
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

func TestSecretRequired(t *testing.T) {
	h := (&Server{Secret: "s3cret"}).Handler()
	next := func(path, secret string) int {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if secret != "" {
			req.Header.Set(secretHeader, secret)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := next("/habits", ""); code != http.StatusUnauthorized {
		t.Errorf("no secret: %d", code)
	}
	if code := next("/habits", "wrong"); code != http.StatusUnauthorized {
		t.Errorf("wrong secret: %d", code)
	}
	// Correct secret gets past auth: an empty body then fails validation.
	if code := next("/habits", "s3cret"); code != http.StatusBadRequest {
		t.Errorf("correct secret: %d", code)
	}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("healthz: %d", rec.Code)
	}
}

func TestTargetMaskIgnoresArchiveRows(t *testing.T) {
	mask := streak.MaskFromDays([]int{1, 3})
	got := targetMask([]streak.Schedule{{From: 10, Mask: mask}, {From: 20, Mask: 0}})
	if got != mask {
		t.Errorf("targetMask = %b, want %b", got, mask)
	}
}
