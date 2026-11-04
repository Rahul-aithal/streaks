package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"streaks/internal/github"
	"streaks/internal/gtasks"
	"streaks/internal/store"
	"streaks/internal/syncer"
)

const (
	sessionCookie   = "streaks_session"
	stateCookie     = "streaks_oauth_state"
	githubCookie    = "streaks_github_state"
	sessionLifetime = 30 * 24 * time.Hour
	stateLifetime   = 10 * time.Minute

	errDenied       = "denied"
	errExpired      = "expired"
	errMissingScope = "missing_scope"
	errUnverified   = "unverified"
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

func (s *stateStore) issue() (string, error) { return s.issueFor("") }

// issueFor makes a state that's only valid for the given owner.
func (s *stateStore) issueFor(owner string) (string, error) {
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, expires := range s.states {
		if now.After(expires) {
			delete(s.states, k)
		}
	}
	s.states[state+"|"+owner] = now.Add(stateLifetime)
	return state, nil
}

// consume reports whether state is valid. It works only once.
func (s *stateStore) consume(state string) bool { return s.consumeFor(state, "") }

func (s *stateStore) consumeFor(state, owner string) bool {
	key := state + "|" + owner
	s.mu.Lock()
	defer s.mu.Unlock()
	expires, ok := s.states[key]
	delete(s.states, key)
	return ok && !s.now().After(expires)
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type sessionKey struct{}

type session struct {
	user      store.User
	tokenHash string
}

func sessionFrom(r *http.Request) session {
	return r.Context().Value(sessionKey{}).(session)
}

// userID is the signed-in user every data query is scoped to.
func userID(r *http.Request) int64 { return sessionFrom(r).user.ID }

// requireSession rejects requests without a live session.
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			writeError(w, http.StatusUnauthorized, "not signed in")
			return
		}
		hash := hashToken(c.Value)
		u, err := s.Store.SessionUser(r.Context(), hash)
		if errors.Is(err, store.ErrNotFound) {
			s.clearCookie(w, sessionCookie)
			writeError(w, http.StatusUnauthorized, "not signed in")
			return
		}
		if err != nil {
			writeStoreError(w, err)
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey{}, session{user: u, tokenHash: hash})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// checkOrigin rejects state-changing requests from other sites. SameSite=Lax
// cookies already stop most of them; this covers the rest.
func (s *Server) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" && origin != s.WebOrigin {
				writeError(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setCookie(w http.ResponseWriter, name, value string, lifetime time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(lifetime.Seconds()),
		HttpOnly: true,
		Secure:   s.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

// startGoogleLogin is a plain link, so it takes no session. The state cookie ties
// the callback to this browser, so nobody can sign you in to their account.
func (s *Server) startGoogleLogin(w http.ResponseWriter, r *http.Request) {
	s.redirectToGoogle(w, r, r.URL.Query().Get("consent") == "1")
}

func (s *Server) redirectToGoogle(w http.ResponseWriter, r *http.Request, forceConsent bool) {
	state, err := s.oauthStates.issue()
	if err != nil {
		redirectLoginError(w, r, errFailed)
		return
	}
	s.setCookie(w, stateCookie, state, stateLifetime)
	http.Redirect(w, r, s.Sync.Google.AuthCodeURL(state, forceConsent), http.StatusFound)
}

func redirectLoginError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?"+url.Values{"error": {code}}.Encode(), http.StatusFound)
}

func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cookie, err := r.Cookie(stateCookie)
	s.clearCookie(w, stateCookie)
	if err != nil || cookie.Value != q.Get("state") || !s.oauthStates.consume(q.Get("state")) {
		redirectLoginError(w, r, errExpired)
		return
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		redirectLoginError(w, r, errDenied)
		return
	}

	identity, err := s.Sync.Google.ExchangeCode(r.Context(), q.Get("code"))
	switch {
	case errors.Is(err, gtasks.ErrMissingScope):
		redirectLoginError(w, r, errMissingScope)
		return
	case errors.Is(err, gtasks.ErrUnverifiedEmail):
		redirectLoginError(w, r, errUnverified)
		return
	case err != nil:
		slog.Error("google login failed", "err", err)
		redirectLoginError(w, r, errFailed)
		return
	}

	sealed := ""
	if identity.RefreshToken != "" {
		if sealed, err = s.Sync.Box.Seal(identity.RefreshToken); err != nil {
			slog.Error("encrypt google token", "err", err)
			redirectLoginError(w, r, errFailed)
			return
		}
	} else {
		// A returning Google user gets no new token. That's fine unless we need one.
		existing, err := s.Store.GetUserBySub(r.Context(), identity.Sub)
		if errors.Is(err, store.ErrNotFound) || (err == nil && existing.NeedsReconnect) {
			s.redirectToGoogle(w, r, true)
			return
		}
		if err != nil {
			slog.Error("look up user", "err", err)
			redirectLoginError(w, r, errFailed)
			return
		}
	}

	id, err := s.Store.SaveSignIn(r.Context(), identity.Sub, identity.Email, identity.Name, identity.AvatarURL, sealed)
	if err != nil {
		slog.Error("save sign-in", "err", err)
		redirectLoginError(w, r, errFailed)
		return
	}
	if err := s.startSession(w, r, id); err != nil {
		slog.Error("start session", "err", err)
		redirectLoginError(w, r, errFailed)
		return
	}
	// Sync now so tasks are there on arrival. A failure is recorded and shown in the app.
	if err := s.Sync.Sync(r.Context(), id, syncer.KindGoogle); err != nil {
		slog.Error("sync after sign-in", "user", id, "err", err)
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// startSession adds the user to this browser's session, if it still has a live
// one, so accounts can be swapped without signing in again.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, id int64) error {
	token := ""
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if _, err := s.Store.SessionUser(r.Context(), hashToken(c.Value)); err == nil {
			token = c.Value
		}
	}
	if token == "" {
		var err error
		if token, err = randomToken(); err != nil {
			return err
		}
	}
	if err := s.Store.SignInToSession(r.Context(), hashToken(token), id, s.Now().Add(sessionLifetime)); err != nil {
		return err
	}
	s.setCookie(w, sessionCookie, token, sessionLifetime)
	return nil
}

type accountJSON struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Active    bool   `json:"active"`
}

// me deliberately exposes no encrypted tokens.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	accounts, err := s.Store.SessionAccounts(r.Context(), sess.tokenHash)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	githubRun, err := s.Store.GetSyncRun(r.Context(), sess.user.ID, syncer.KindGitHub)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	googleRun, err := s.Store.GetSyncRun(r.Context(), sess.user.ID, syncer.KindGoogle)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	list := make([]accountJSON, 0, len(accounts))
	for _, a := range accounts {
		list = append(list, accountJSON{
			ID: a.ID, Email: a.Email, Name: a.Name, AvatarURL: a.AvatarURL, Active: a.ID == sess.user.ID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": list,
		"google":   map[string]any{"needs_reconnect": sess.user.NeedsReconnect, "sync": googleRun},
		"github": map[string]any{
			"login": sess.user.GitHubLogin, "sync": githubRun, "connect_enabled": s.Sync.GitHubOAuth != nil,
		},
	})
}

func (s *Server) switchAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID int64 `json:"user_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	err := s.Store.SwitchSessionUser(r.Context(), sessionFrom(r).tokenHash, req.UserID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusForbidden, "that account isn't signed in here")
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// signOut removes the active account, moving to the next one if there is one.
func (s *Server) signOut(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	remaining, err := s.Store.SignOutUser(r.Context(), sess.tokenHash, sess.user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !remaining {
		s.clearCookie(w, sessionCookie)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"signed_in": remaining})
}

func (s *Server) signOutAll(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteSession(r.Context(), sessionFrom(r).tokenHash); err != nil {
		writeStoreError(w, err)
		return
	}
	s.clearCookie(w, sessionCookie)
	w.WriteHeader(http.StatusNoContent)
}

// deleteAccount removes the active user and all their data, keeping any other
// accounts in this browser signed in.
func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	remaining, err := s.Store.SignOutUser(r.Context(), sess.tokenHash, sess.user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !remaining {
		s.clearCookie(w, sessionCookie)
	}
	if err := s.Sync.DeleteAccount(r.Context(), sess.user); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"signed_in": remaining})
}

func redirectGitHubResult(w http.ResponseWriter, r *http.Request, result string) {
	http.Redirect(w, r, "/settings?"+url.Values{"github": {result}}.Encode(), http.StatusFound)
}

// startGitHubConnect is a plain link. The state is tied to this browser by a
// cookie and to the signed-in user, so GitHub is linked to the account that asked.
func (s *Server) startGitHubConnect(w http.ResponseWriter, r *http.Request) {
	if s.Sync.GitHubOAuth == nil {
		redirectGitHubResult(w, r, errFailed)
		return
	}
	state, err := s.oauthStates.issueFor(strconv.FormatInt(userID(r), 10))
	if err != nil {
		redirectGitHubResult(w, r, errFailed)
		return
	}
	s.setCookie(w, githubCookie, state, stateLifetime)
	http.Redirect(w, r, s.Sync.GitHubOAuth.AuthorizeURL(state), http.StatusFound)
}

func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	stateCookieValue, stateErr := r.Cookie(githubCookie)
	s.clearCookie(w, githubCookie)

	session, err := r.Cookie(sessionCookie)
	if err != nil {
		redirectLoginError(w, r, errExpired)
		return
	}
	user, err := s.Store.SessionUser(r.Context(), hashToken(session.Value))
	if errors.Is(err, store.ErrNotFound) {
		redirectLoginError(w, r, errExpired)
		return
	}
	if err != nil {
		slog.Error("github callback session", "err", err)
		redirectGitHubResult(w, r, errFailed)
		return
	}

	owner := strconv.FormatInt(user.ID, 10)
	if stateErr != nil || stateCookieValue.Value != q.Get("state") || !s.oauthStates.consumeFor(q.Get("state"), owner) {
		redirectGitHubResult(w, r, errExpired)
		return
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		redirectGitHubResult(w, r, errDenied)
		return
	}
	if _, err := s.Sync.ConnectGitHub(r.Context(), user.ID, q.Get("code")); err != nil {
		slog.Error("connect GitHub", "user", user.ID, "err", err)
		redirectGitHubResult(w, r, errFailed)
		return
	}
	redirectGitHubResult(w, r, "connected")
}

func (s *Server) linkGitHub(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	login, err := s.Sync.LinkGitHub(r.Context(), userID(r), token)
	if errors.Is(err, github.ErrBadToken) {
		writeError(w, http.StatusUnprocessableEntity, "GitHub rejected this token. Check it's correct and hasn't expired.")
		return
	}
	if err != nil {
		slog.Error("link GitHub", "user", userID(r), "err", err)
		writeError(w, http.StatusBadGateway, "Couldn't reach GitHub or save the token. Try again.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"login": login})
}

func (s *Server) unlinkGitHub(w http.ResponseWriter, r *http.Request) {
	if err := s.Sync.UnlinkGitHub(r.Context(), sessionFrom(r).user); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
