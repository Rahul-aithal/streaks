// Package syncer runs GitHub and Google Tasks syncs in-process.
package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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

// ErrNotConfigured means the credentials for a sync aren't set.
var ErrNotConfigured = errors.New("sync not configured")

type Syncer struct {
	Store    *store.Store
	GitHub   *github.Client // Nil when not configured.
	Google   *gtasks.Client // Nil when not configured.
	Box      *secretbox.Box // Encrypts Google refresh tokens; set with Google.
	Interval time.Duration
	Now      func() time.Time

	mu          sync.Mutex // Serializes syncs, so a manual one can't overlap a scheduled one.
	lastAttempt map[string]time.Time
}

// Run syncs anything stale at startup, then keeps checking until ctx ends. The
// check compares wall-clock times held in memory, so it survives laptop sleep
// (which a plain long ticker wouldn't) and never touches the database between
// syncs, which lets Neon scale to zero.
func (s *Syncer) Run(ctx context.Context) {
	s.lastAttempt = s.loadLastSuccess(ctx)
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

func (s *Syncer) kinds() map[string]bool {
	return map[string]bool{KindGitHub: s.GitHub != nil, KindGoogle: s.Google != nil}
}

func (s *Syncer) loadLastSuccess(ctx context.Context) map[string]time.Time {
	last := map[string]time.Time{}
	for kind, enabled := range s.kinds() {
		if !enabled {
			continue
		}
		run, err := s.Store.GetSyncRun(ctx, kind)
		if err != nil {
			slog.Warn("read sync state", "kind", kind, "err", err)
			continue
		}
		if run.LastSuccessAt != nil {
			last[kind] = *run.LastSuccessAt
		}
	}
	return last
}

func (s *Syncer) runDue(ctx context.Context) {
	for kind, enabled := range s.kinds() {
		if !enabled || ctx.Err() != nil {
			continue
		}
		s.mu.Lock()
		last := s.lastAttempt[kind]
		s.mu.Unlock()
		if !last.IsZero() && s.Now().Sub(last) < s.Interval {
			continue
		}
		if err := s.Sync(ctx, kind); err != nil {
			slog.Error("sync failed", "kind", kind, "err", err)
		}
	}
}

// Sync runs one sync now and records the outcome. A failed attempt waits for
// the next interval rather than retrying every check.
func (s *Syncer) Sync(ctx context.Context, kind string) error {
	if enabled, known := s.kinds()[kind]; !known || !enabled {
		return ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, syncLimit)
	defer cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastAttempt == nil {
		s.lastAttempt = map[string]time.Time{}
	}
	s.lastAttempt[kind] = s.Now()

	var err error
	switch kind {
	case KindGitHub:
		err = s.syncGitHub(ctx)
	case KindGoogle:
		err = s.syncGoogle(ctx)
	}
	if recordErr := s.Store.RecordSync(ctx, kind, err); recordErr != nil {
		slog.Warn("record sync state", "kind", kind, "err", recordErr)
	}
	if err == nil {
		slog.Info("sync complete", "kind", kind)
	}
	return err
}

func (s *Syncer) syncGitHub(ctx context.Context) error {
	events, err := s.GitHub.FetchActivity(ctx)
	if err != nil {
		return err
	}
	return s.Store.SaveGitHubEvents(ctx, events)
}

// syncGoogle syncs every account. One account failing doesn't stop the others,
// and it keeps its cached tasks so they still count until it recovers.
func (s *Syncer) syncGoogle(ctx context.Context) error {
	accounts, err := s.Store.ListGoogleAccounts(ctx)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		return nil
	}
	today := streak.DayOf(s.Now())
	var errs []error
	for _, account := range accounts {
		syncErr := s.syncGoogleAccount(ctx, account, today)
		reconnect := errors.Is(syncErr, gtasks.ErrReconnect) || errors.Is(syncErr, errUnreadableToken)
		if err := s.Store.RecordAccountSync(ctx, account.ID, syncErr, reconnect); err != nil {
			slog.Warn("record account sync", "account", account.ID, "err", err)
		}
		if syncErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", account.Email, syncErr))
		}
	}
	if err := s.saveSnapshot(ctx, today); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

var errUnreadableToken = errors.New("stored token can't be decrypted (did GOOGLE_TOKEN_KEY change?): reconnect this account")

func (s *Syncer) syncGoogleAccount(ctx context.Context, account store.GoogleAccount, today streak.Day) error {
	token, err := s.Box.Open(account.RefreshTokenEnc)
	if err != nil {
		return errUnreadableToken
	}
	tasks, err := s.Google.FetchTasks(ctx, token, today)
	if err != nil {
		return err
	}
	return s.Store.SaveAccountTasks(ctx, account.ID, tasks)
}

func (s *Syncer) saveSnapshot(ctx context.Context, today streak.Day) error {
	tasks, err := s.Store.ListTasks(ctx)
	if err != nil {
		return err
	}
	_, snap := gtasks.ForToday(tasks, today)
	return s.Store.SaveTaskSnapshot(ctx, today, snap)
}

// DisconnectGoogle revokes an account's access, deletes it with its cached tasks,
// and corrects today's snapshot. A failed revoke doesn't block the disconnect.
func (s *Syncer) DisconnectGoogle(ctx context.Context, id int64) error {
	if s.Google == nil {
		return ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, syncLimit)
	defer cancel()

	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, err := s.Store.ListGoogleAccounts(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(accounts, func(a store.GoogleAccount) bool { return a.ID == id })
	if i < 0 {
		return store.ErrNotFound
	}
	if token, err := s.Box.Open(accounts[i].RefreshTokenEnc); err != nil {
		slog.Warn("skip revoke: token unreadable", "account", id)
	} else if err := s.Google.Revoke(ctx, token); err != nil {
		slog.Warn("revoke failed; remove access at myaccount.google.com/permissions", "account", id, "err", err)
	}
	if err := s.Store.DeleteGoogleAccount(ctx, id); err != nil {
		return err
	}
	return s.saveSnapshot(ctx, streak.DayOf(s.Now()))
}
