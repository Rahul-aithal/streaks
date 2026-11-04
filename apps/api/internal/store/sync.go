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

// SaveGitHubEvents stores events and checks in every active GitHub habit on each
// day with activity. It is safe to re-run: events are keyed by GitHub's ID.
func (s *Store) SaveGitHubEvents(ctx context.Context, events []GitHubEvent) error {
	return s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		ids, types, times, days := make([]string, len(events)), make([]string, len(events)),
			make([]string, len(events)), make([]string, len(events))
		for i, e := range events {
			ids[i], types[i] = e.ID, e.Type
			times[i] = e.OccurredAt.UTC().Format(time.RFC3339)
			days[i] = streak.DayOf(e.OccurredAt).String()
		}
		// One statement rather than one round trip per row: Neon may be far away.
		if _, err := tx.Exec(ctx, `INSERT INTO github_events (id, type, occurred_at, day)
			SELECT * FROM unnest($1::text[], $2::text[], $3::text[]::timestamptz[], $4::text[]::date[])
			ON CONFLICT (id) DO NOTHING`, ids, types, times, days); err != nil {
			return err
		}
		return applyGitHubCheckIns(ctx, tx)
	})
}

func applyGitHubCheckIns(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `INSERT INTO check_ins (habit_id, day)
		SELECT h.id, e.day
		FROM habits h CROSS JOIN (SELECT DISTINCT day FROM github_events) e
		WHERE h.source = 'github' AND h.archived_at IS NULL
		ON CONFLICT (habit_id, day) DO NOTHING`)
	return err
}

// ContributionCounts returns per-day activity counts in an inclusive range.
func (s *Store) ContributionCounts(ctx context.Context, from, to streak.Day) (map[streak.Day]int, error) {
	counts := map[streak.Day]int{}
	err := s.run(ctx, func(ctx context.Context) error {
		clear(counts)
		rows, err := s.pool.Query(ctx, `SELECT day, count(*) FROM github_events
			WHERE day BETWEEN $1::date AND $2::date GROUP BY day`, from, to)
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

// SaveTasks replaces the task cache and upserts the day's snapshot in one transaction.
func (s *Store) SaveTasks(ctx context.Context, tasks []Task, day streak.Day, snap TaskSnapshot) error {
	return s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM tasks`); err != nil {
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
		if _, err := tx.Exec(ctx, `INSERT INTO tasks (google_id, list_id, title, status, due, completed_at)
			SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[],
				$5::text[]::date[], $6::text[]::timestamptz[])`,
			ids, lists, titles, statuses, dues, completed); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO task_snapshots (day, total, completed, overdue)
			VALUES ($1::date, $2, $3, $4)
			ON CONFLICT (day) DO UPDATE SET total = EXCLUDED.total, completed = EXCLUDED.completed,
				overdue = EXCLUDED.overdue, taken_at = now()`,
			day, snap.Total, snap.Completed, snap.Overdue)
		return err
	})
}

// ListTasks returns all cached tasks.
func (s *Store) ListTasks(ctx context.Context) ([]Task, error) {
	var tasks []Task
	err := s.run(ctx, func(ctx context.Context) error {
		rows, err := s.pool.Query(ctx, `SELECT google_id, list_id, title, status, due, completed_at
			FROM tasks ORDER BY due NULLS LAST, title`)
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

// GetSyncRun returns the last outcome, or the zero value if it has never run.
func (s *Store) GetSyncRun(ctx context.Context, kind string) (SyncRun, error) {
	var r SyncRun
	err := s.run(ctx, func(ctx context.Context) error {
		err := s.pool.QueryRow(ctx, `SELECT last_success_at, last_error FROM sync_runs WHERE kind = $1`, kind).
			Scan(&r.LastSuccessAt, &r.LastError)
		if errors.Is(err, pgx.ErrNoRows) {
			r = SyncRun{}
			return nil
		}
		return err
	})
	return r, err
}

// RecordSync stores the outcome of a sync. A failure keeps the last success time.
func (s *Store) RecordSync(ctx context.Context, kind string, syncErr error) error {
	return s.run(ctx, func(ctx context.Context) error {
		if syncErr == nil {
			_, err := s.pool.Exec(ctx, `INSERT INTO sync_runs (kind, last_success_at) VALUES ($1, now())
				ON CONFLICT (kind) DO UPDATE SET last_success_at = now(), last_error = NULL, updated_at = now()`, kind)
			return err
		}
		_, err := s.pool.Exec(ctx, `INSERT INTO sync_runs (kind, last_error) VALUES ($1, $2)
			ON CONFLICT (kind) DO UPDATE SET last_error = EXCLUDED.last_error, updated_at = now()`,
			kind, syncErr.Error())
		return err
	})
}
