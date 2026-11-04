// Package syncer runs GitHub and Google Tasks syncs in-process, for every user.
package syncer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"streaks/internal/github"
	"streaks/internal/gtasks"
	"streaks/internal/secretbox"
	"streaks/internal/store"
	"streaks/internal/streak"
)

const (
	KindGitHub = "github"
	KindGoogle = "google_tasks"

	checkEvery = 15 * time.Minute
	syncLimit  = 2 * time.Minute
)

// ErrNotConfigured means this user hasn't linked the service.
var ErrNotConfigured = errors.New("not linked")

// ErrUnreadableToken means a stored token can't be decrypted.
var ErrUnreadableToken = errors.New("stored token can't be decrypted (did GOOGLE_TOKEN_KEY change?)")

type Syncer struct {
	Store         *store.Store
	Google        *gtasks.Client
	Box           *secretbox.Box
	GitHubBaseURL string
	Interval      time.Duration
	Now           func() time.Time

	mu      sync.Mutex // Serializes syncs, so a manual one can't overlap a scheduled one.
	lastRun map[string]time.Time
}

// Run syncs anything stale at startup, then keeps checking until ctx ends. The
// check compares wall-clock times held in memory, so it survives laptop sleep
// (which a plain long ticker wouldn't) and never touches the database between
// syncs, which lets Neon scale to zero.
func (s *Syncer) Run(ctx context.Context) {
	s.lastRun = map[string]time.Time{}
	for _, kind := range []string{KindGitHub, KindGoogle} {
		at, err := s.Store.LatestSyncAt(ctx, kind)
		if err != nil {
			slog.Warn("read sync state", "kind", kind, "err", err)
			continue
		}
		if at != nil {
			s.lastRun[kind] = *at
		}
	}
	s.runDue(ctx)

	ticker := time.NewTicker(checkEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDue(ctx)
		}
	}
}

// runDue syncs all users for each stale kind. A failure waits for the next
// interval rather than retrying every check.
func (s *Syncer) runDue(ctx context.Context) {
	for _, kind := range []string{KindGitHub, KindGoogle} {
		if ctx.Err() != nil {
			return
		}
		if last := s.lastRun[kind]; !last.IsZero() && s.Now().Sub(last) < s.Interval {
			continue
		}
		s.lastRun[kind] = s.Now()
		users, err := s.Store.ListUsers(ctx)
		if err != nil {
			slog.Error("list users for sync", "kind", kind, "err", err)
			continue
		}
		for _, u := range users {
			if err := s.syncUser(ctx, u, kind); err != nil && !errors.Is(err, ErrNotConfigured) {
				slog.Error("sync failed", "kind", kind, "user", u.ID, "err", err)
			}
		}
	}
}

// Sync runs one user's sync now and records the outcome.
func (s *Syncer) Sync(ctx context.Context, userID int64, kind string) error {
	u, err := s.Store.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	return s.syncUser(ctx, u, kind)
}

func (s *Syncer) syncUser(ctx context.Context, u store.User, kind string) error {
	if kind == KindGitHub && u.GitHubTokenEnc == nil {
		return ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, syncLimit)
	defer cancel()

	s.mu.Lock()
	defer s.mu.Unlock()

	var err error
	switch kind {
	case KindGitHub:
		err = s.syncGitHub(ctx, u)
	case KindGoogle:
		err = s.syncGoogle(ctx, u)
		reconnect := errors.Is(err, gtasks.ErrReconnect) || errors.Is(err, ErrUnreadableToken)
		if flagErr := s.Store.SetNeedsReconnect(ctx, u.ID, reconnect); flagErr != nil {
			slog.Warn("record reconnect state", "user", u.ID, "err", flagErr)
		}
	default:
		return ErrNotConfigured
	}
	if recordErr := s.Store.RecordSync(ctx, u.ID, kind, err); recordErr != nil {
		slog.Warn("record sync state", "kind", kind, "user", u.ID, "err", recordErr)
	}
	if err == nil {
		slog.Info("sync complete", "kind", kind, "user", u.ID)
	}
	return err
}

func (s *Syncer) syncGitHub(ctx context.Context, u store.User) error {
	token, err := s.Box.Open(*u.GitHubTokenEnc)
	if err != nil {
		return ErrUnreadableToken
	}
	login := ""
	if u.GitHubLogin != nil {
		login = *u.GitHubLogin
	}
	events, err := github.NewClient(s.GitHubBaseURL, token).FetchActivity(ctx, login)
	if err != nil {
		return err
	}
	return s.Store.SaveGitHubEvents(ctx, u.ID, events)
}

func (s *Syncer) syncGoogle(ctx context.Context, u store.User) error {
	token, err := s.Box.Open(u.RefreshTokenEnc)
	if err != nil {
		return ErrUnreadableToken
	}
	today := streak.DayOf(s.Now())
	tasks, err := s.Google.FetchTasks(ctx, token, today)
	if err != nil {
		return err
	}
	_, snap := gtasks.ForToday(tasks, today)
	return s.Store.SaveTasks(ctx, u.ID, tasks, today, snap)
}

// LinkGitHub verifies a token, stores it encrypted, and runs a first sync. A
// failed first sync is recorded but doesn't undo the link.
func (s *Syncer) LinkGitHub(ctx context.Context, userID int64, token string) (string, error) {
	login, err := github.NewClient(s.GitHubBaseURL, token).Login(ctx)
	if err != nil {
		return "", err
	}
	sealed, err := s.Box.Seal(token)
	if err != nil {
		return "", err
	}
	if err := s.Store.SetGitHub(ctx, userID, login, sealed); err != nil {
		return "", err
	}
	if err := s.Sync(ctx, userID, KindGitHub); err != nil {
		slog.Error("first GitHub sync", "user", userID, "err", err)
	}
	return login, nil
}

// DeleteAccount revokes Google access and deletes the user and all their data. A
// failed revoke doesn't block deletion.
func (s *Syncer) DeleteAccount(ctx context.Context, u store.User) error {
	ctx, cancel := context.WithTimeout(ctx, syncLimit)
	defer cancel()

	s.mu.Lock()
	defer s.mu.Unlock()

	if token, err := s.Box.Open(u.RefreshTokenEnc); err != nil {
		slog.Warn("skip revoke: token unreadable", "user", u.ID)
	} else if err := s.Google.Revoke(ctx, token); err != nil {
		slog.Warn("revoke failed; remove access at myaccount.google.com/permissions", "user", u.ID, "err", err)
	}
	return s.Store.DeleteUser(ctx, u.ID)
}
