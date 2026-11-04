// Package syncer runs GitHub and Google Tasks syncs in-process.
package syncer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"streaks/internal/github"
	"streaks/internal/gtasks"
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

func (s *Syncer) syncGoogle(ctx context.Context) error {
	today := streak.DayOf(s.Now())
	tasks, err := s.Google.FetchTasks(ctx, today)
	if err != nil {
		return err
	}
	_, snap := gtasks.ForToday(tasks, today)
	return s.Store.SaveTasks(ctx, tasks, today, snap)
}
