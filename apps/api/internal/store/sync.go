package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"streaks/internal/streak"
)

// GitHubEvent is one counted activity event.
type GitHubEvent struct {
	ID         string
	Type       string
	OccurredAt time.Time
}

// SaveGitHubEvents stores a user's events and checks in their active GitHub habits
// on each day with activity. It is safe to re-run: events are keyed by GitHub's ID.
func (s *Store) SaveGitHubEvents(ctx context.Context, userID int64, events []GitHubEvent) error {
	return s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		ids, types, times, days := make([]string, len(events)), make([]string, len(events)),
			make([]string, len(events)), make([]string, len(events))
		for i, e := range events {
			ids[i], types[i] = e.ID, e.Type
			times[i] = e.OccurredAt.UTC().Format(time.RFC3339)
			days[i] = streak.DayOf(e.OccurredAt).String()
		}
		// One statement rather than one round trip per row: Neon may be far away.
		if _, err := tx.Exec(ctx, `INSERT INTO github_events (user_id, id, type, occurred_at, day)
			SELECT $1::bigint, * FROM unnest($2::text[], $3::text[], $4::text[]::timestamptz[], $5::text[]::date[])
			ON CONFLICT (user_id, id) DO NOTHING`, userID, ids, types, times, days); err != nil {
			return err
		}
		return applyGitHubCheckIns(ctx, tx, userID)
	})
}

func applyGitHubCheckIns(ctx context.Context, tx pgx.Tx, userID int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO check_ins (habit_id, day)
		SELECT h.id, e.day
		FROM habits h
		CROSS JOIN (SELECT DISTINCT day FROM github_events WHERE user_id = $1) e
		WHERE h.user_id = $1 AND h.source = 'github' AND h.archived_at IS NULL
		ON CONFLICT (habit_id, day) DO NOTHING`, userID)
	return err
}

// ContributionCounts returns a user's per-day activity counts in an inclusive range.
func (s *Store) ContributionCounts(ctx context.Context, userID int64, from, to streak.Day) (map[streak.Day]int, error) {
	counts := map[streak.Day]int{}
	err := s.run(ctx, func(ctx context.Context) error {
		clear(counts)
		rows, err := s.pool.Query(ctx, `SELECT day, count(*) FROM github_events
			WHERE user_id = $1 AND day BETWEEN $2::date AND $3::date GROUP BY day`, userID, from, to)
		if err != nil {
			return err
		}
		var day streak.Day
		var n int
		_, err = pgx.ForEachRow(rows, []any{&day, &n}, func() error {
			counts[day] = n
			return nil
		})
		return err
	})
	return counts, err
}

// Task is a cached Google Task.
type Task struct {
	GoogleID    string
	ListID      string
	Title       string
	Status      string
	Due         *streak.Day
	CompletedAt *time.Time
}

// TaskSnapshot holds one day's task counts.
type TaskSnapshot struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Overdue   int `json:"overdue"`
}

// SaveTasks replaces a user's cached tasks and upserts the day's snapshot together.
func (s *Store) SaveTasks(ctx context.Context, userID int64, tasks []Task, day streak.Day, snap TaskSnapshot) error {
	return s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM tasks WHERE user_id = $1`, userID); err != nil {
			return err
		}
		n := len(tasks)
		ids, lists, titles, statuses := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		dues, completed := make([]*string, n), make([]*string, n)
		for i, t := range tasks {
			ids[i], lists[i], titles[i], statuses[i] = t.GoogleID, t.ListID, t.Title, t.Status
			if t.Due != nil {
				v := t.Due.String()
				dues[i] = &v
			}
			if t.CompletedAt != nil {
				v := t.CompletedAt.UTC().Format(time.RFC3339)
				completed[i] = &v
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tasks (user_id, google_id, list_id, title, status, due, completed_at)
			SELECT $1::bigint, * FROM unnest($2::text[], $3::text[], $4::text[], $5::text[],
				$6::text[]::date[], $7::text[]::timestamptz[])`,
			userID, ids, lists, titles, statuses, dues, completed); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO task_snapshots (user_id, day, total, completed, overdue)
			VALUES ($1, $2::date, $3, $4, $5)
			ON CONFLICT (user_id, day) DO UPDATE SET total = EXCLUDED.total, completed = EXCLUDED.completed,
				overdue = EXCLUDED.overdue, taken_at = now()`,
			userID, day, snap.Total, snap.Completed, snap.Overdue)
		return err
	})
}

// ListTasks returns a user's cached tasks.
func (s *Store) ListTasks(ctx context.Context, userID int64) ([]Task, error) {
	var tasks []Task
	err := s.run(ctx, func(ctx context.Context) error {
		rows, err := s.pool.Query(ctx, `SELECT google_id, list_id, title, status, due, completed_at
			FROM tasks WHERE user_id = $1 ORDER BY due NULLS LAST, title`, userID)
		if err != nil {
			return err
		}
		tasks, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Task, error) {
			var t Task
			err := r.Scan(&t.GoogleID, &t.ListID, &t.Title, &t.Status, &t.Due, &t.CompletedAt)
			return t, err
		})
		return err
	})
	return tasks, err
}

// SyncRun is the last recorded outcome of a sync kind.
type SyncRun struct {
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     *string    `json:"last_error"`
}

// GetSyncRun returns a user's last outcome, or the zero value if it has never run.
func (s *Store) GetSyncRun(ctx context.Context, userID int64, kind string) (SyncRun, error) {
	var r SyncRun
	err := s.run(ctx, func(ctx context.Context) error {
		err := s.pool.QueryRow(ctx, `SELECT last_success_at, last_error FROM sync_runs
			WHERE user_id = $1 AND kind = $2`, userID, kind).Scan(&r.LastSuccessAt, &r.LastError)
		if errors.Is(err, pgx.ErrNoRows) {
			r = SyncRun{}
			return nil
		}
		return err
	})
	return r, err
}

// LatestSyncAt returns when any user last synced a kind, to decide if a run is due at startup.
func (s *Store) LatestSyncAt(ctx context.Context, kind string) (*time.Time, error) {
	var at *time.Time
	err := s.run(ctx, func(ctx context.Context) error {
		return s.pool.QueryRow(ctx, `SELECT max(last_success_at) FROM sync_runs WHERE kind = $1`, kind).Scan(&at)
	})
	return at, err
}

// RecordSync stores a sync's outcome. A failure keeps the last success time.
func (s *Store) RecordSync(ctx context.Context, userID int64, kind string, syncErr error) error {
	return s.run(ctx, func(ctx context.Context) error {
		if syncErr == nil {
			_, err := s.pool.Exec(ctx, `INSERT INTO sync_runs (user_id, kind, last_success_at) VALUES ($1, $2, now())
				ON CONFLICT (user_id, kind) DO UPDATE SET last_success_at = now(), last_error = NULL, updated_at = now()`,
				userID, kind)
			return err
		}
		_, err := s.pool.Exec(ctx, `INSERT INTO sync_runs (user_id, kind, last_error) VALUES ($1, $2, $3)
			ON CONFLICT (user_id, kind) DO UPDATE SET last_error = EXCLUDED.last_error, updated_at = now()`,
			userID, kind, syncErr.Error())
		return err
	})
}
