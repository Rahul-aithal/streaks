package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
)

// requireJobsSecret lets a request through only with "Authorization: Bearer
// <JobsSecret>". Hashing both sides first makes the comparison constant-time
// whatever the length.
func (s *Server) requireJobsSecret(next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(s.JobsSecret))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		got := sha256.Sum256([]byte(token))
		if !ok || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid jobs secret")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// runReminders sends the reminder whose window is open now, if any.
func (s *Server) runReminders(w http.ResponseWriter, r *http.Request) {
	if s.Reminders == nil {
		writeJSON(w, http.StatusOK, map[string]any{"slot": nil, "skipped": "email is not configured"})
		return
	}
	slot, err := s.Reminders.RunDue(r.Context())
	if err != nil {
		slog.Error("reminder job", "slot", slot, "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var out any
	if slot != "" {
		out = slot
	}
	writeJSON(w, http.StatusOK, map[string]any{"slot": out})
}

// runSync syncs GitHub and Google Tasks for every user.
func (s *Server) runSync(w http.ResponseWriter, r *http.Request) {
	if err := s.Sync.SyncAll(r.Context()); err != nil {
		slog.Error("sync job", "err", err)
		writeError(w, http.StatusInternalServerError, "sync failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
