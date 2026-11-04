package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"streaks/internal/gtasks"
	"streaks/internal/store"
	"streaks/internal/syncer"
)

const (
	stateLifetime   = 10 * time.Minute
	accountsPage    = "/accounts"
	errDenied       = "denied"
	errExpired      = "expired"
	errMissingScope = "missing_scope"
	errNoToken      = "no_refresh_token"
	errFailed       = "failed"
)

// stateStore holds one-time OAuth state values in memory. A restart mid-login
// just means trying again.
type stateStore struct {
	mu     sync.Mutex
	now    func() time.Time
	states map[string]time.Time
}

func newStateStore(now func() time.Time) *stateStore {
	return &stateStore{now: now, states: map[string]time.Time{}}
}

func (s *stateStore) issue() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	state := hex.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, expires := range s.states {
		if now.After(expires) {
			delete(s.states, k)
		}
	}
	s.states[state] = now.Add(stateLifetime)
	return state, nil
}

// consume reports whether state is valid. It works only once.
func (s *stateStore) consume(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	expires, ok := s.states[state]
	delete(s.states, state)
	return ok && !s.now().After(expires)
}

type accountJSON struct {
	ID             int64      `json:"id"`
	Email          string     `json:"email"`
	LastSuccessAt  *time.Time `json:"last_success_at"`
	LastError      *string    `json:"last_error"`
	NeedsReconnect bool       `json:"needs_reconnect"`
	CreatedAt      time.Time  `json:"created_at"`
}

// toAccountsJSON deliberately leaves out the encrypted token.
func toAccountsJSON(accounts []store.GoogleAccount) []accountJSON {
	out := make([]accountJSON, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, accountJSON{
			ID: a.ID, Email: a.Email, LastSuccessAt: a.LastSuccessAt, LastError: a.LastError,
			NeedsReconnect: a.NeedsReconnect, CreatedAt: a.CreatedAt,
		})
	}
	return out
}

func (s *Server) listGoogleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.Store.ListGoogleAccounts(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": s.Sync.Google != nil,
		"accounts":   toAccountsJSON(accounts),
	})
}

// connectGoogle returns Google's login URL. The browser navigates there itself,
// because a plain navigation couldn't carry the secret header to this route.
func (s *Server) connectGoogle(w http.ResponseWriter, _ *http.Request) {
	if s.Sync.Google == nil {
		writeError(w, http.StatusServiceUnavailable, "Google credentials are not set")
		return
	}
	state, err := s.oauthStates.issue()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start login")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": s.Sync.Google.AuthCodeURL(state)})
}

func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	finish := func(key, value string) {
		http.Redirect(w, r, accountsPage+"?"+url.Values{key: {value}}.Encode(), http.StatusFound)
	}
	fail := func(code string) { finish("google_error", code) }

	q := r.URL.Query()
	if s.Sync.Google == nil || !s.oauthStates.consume(q.Get("state")) {
		fail(errExpired)
		return
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		fail(errDenied)
		return
	}

	identity, err := s.Sync.Google.ExchangeCode(r.Context(), q.Get("code"))
	switch {
	case errors.Is(err, gtasks.ErrMissingScope):
		fail(errMissingScope)
		return
	case errors.Is(err, gtasks.ErrNoRefreshToken):
		fail(errNoToken)
		return
	case err != nil:
		slog.Error("google login failed", "err", err)
		fail(errFailed)
		return
	}

	sealed, err := s.Sync.Box.Seal(identity.RefreshToken)
	if err != nil {
		slog.Error("encrypt google token", "err", err)
		fail(errFailed)
		return
	}
	if _, err := s.Store.UpsertGoogleAccount(r.Context(), identity.Sub, identity.Email, sealed); err != nil {
		slog.Error("save google account", "err", err)
		fail(errFailed)
		return
	}
	// Sync now so tasks are there on arrival. A failure is recorded on the account
	// and shown on the page, and the account is still connected.
	if err := s.Sync.Sync(r.Context(), syncer.KindGoogle); err != nil {
		slog.Error("sync after connect", "err", err)
	}
	finish("connected", "1")
}

func (s *Server) disconnectGoogle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	err = s.Sync.DisconnectGoogle(r.Context(), id)
	switch {
	case errors.Is(err, syncer.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "Google credentials are not set")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "account not found")
	case err != nil:
		writeStoreError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
