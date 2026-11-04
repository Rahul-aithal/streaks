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
	AccountEmail string // Set when read back; ignored when saving.
	GoogleID     string
	ListID       string
	Title        string
	Status       string
	Due          *streak.Day
	CompletedAt  *time.Time
}

// TaskSnapshot holds one day's task counts.
type TaskSnapshot struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Overdue   int `json:"overdue"`
}

// SaveAccountTasks replaces one account's cached tasks. Other accounts are untouched.
func (s *Store) SaveAccountTasks(ctx context.Context, accountID int64, tasks []Task) error {
	return s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM tasks WHERE account_id = $1`, accountID); err != nil {
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
		_, err := tx.Exec(ctx, `INSERT INTO tasks (account_id, google_id, list_id, title, status, due, completed_at)
			SELECT $1::bigint, * FROM unnest($2::text[], $3::text[], $4::text[], $5::text[],
				$6::text[]::date[], $7::text[]::timestamptz[])`,
			accountID, ids, lists, titles, statuses, dues, completed)
		return err
	})
}

// SaveTaskSnapshot upserts one day's combined task counts.
func (s *Store) SaveTaskSnapshot(ctx context.Context, day streak.Day, snap TaskSnapshot) error {
	return s.run(ctx, func(ctx context.Context) error {
		_, err := s.pool.Exec(ctx, `INSERT INTO task_snapshots (day, total, completed, overdue)
			VALUES ($1::date, $2, $3, $4)
			ON CONFLICT (day) DO UPDATE SET total = EXCLUDED.total, completed = EXCLUDED.completed,
				overdue = EXCLUDED.overdue, taken_at = now()`,
			day, snap.Total, snap.Completed, snap.Overdue)
		return err
	})
}

// ListTasks returns all cached tasks across accounts.
func (s *Store) ListTasks(ctx context.Context) ([]Task, error) {
	var tasks []Task
	err := s.run(ctx, func(ctx context.Context) error {
		rows, err := s.pool.Query(ctx, `SELECT a.email, t.google_id, t.list_id, t.title, t.status, t.due, t.completed_at
			FROM tasks t JOIN google_accounts a ON a.id = t.account_id
			ORDER BY t.due NULLS LAST, t.title`)
		if err != nil {
			return err
		}
		tasks, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Task, error) {
			var t Task
			err := r.Scan(&t.AccountEmail, &t.GoogleID, &t.ListID, &t.Title, &t.Status, &t.Due, &t.CompletedAt)
			return t, err
		})
		return err
	})
	return tasks, err
}

// GoogleAccount is a connected Google account.
type GoogleAccount struct {
	ID              int64
	Email           string
	RefreshTokenEnc string
	LastSuccessAt   *time.Time
	LastError       *string
	NeedsReconnect  bool
	CreatedAt       time.Time
}

// UpsertGoogleAccount connects an account, or refreshes it if already connected.
func (s *Store) UpsertGoogleAccount(ctx context.Context, sub, email, refreshTokenEnc string) (int64, error) {
	var id int64
	err := s.run(ctx, func(ctx context.Context) error {
		return s.pool.QueryRow(ctx, `INSERT INTO google_accounts (google_sub, email, refresh_token_enc)
			VALUES ($1, $2, $3)
			ON CONFLICT (google_sub) DO UPDATE SET email = EXCLUDED.email,
				refresh_token_enc = EXCLUDED.refresh_token_enc,
				last_error = NULL, needs_reconnect = false, updated_at = now()
			RETURNING id`, sub, email, refreshTokenEnc).Scan(&id)
	})
	return id, err
}

// ListGoogleAccounts returns all connected accounts, oldest first.
func (s *Store) ListGoogleAccounts(ctx context.Context) ([]GoogleAccount, error) {
	var accounts []GoogleAccount
	err := s.run(ctx, func(ctx context.Context) error {
		rows, err := s.pool.Query(ctx, `SELECT id, email, refresh_token_enc, last_success_at, last_error,
			needs_reconnect, created_at FROM google_accounts ORDER BY created_at, id`)
		if err != nil {
			return err
		}
		accounts, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (GoogleAccount, error) {
			var a GoogleAccount
			err := r.Scan(&a.ID, &a.Email, &a.RefreshTokenEnc, &a.LastSuccessAt, &a.LastError, &a.NeedsReconnect, &a.CreatedAt)
			return a, err
		})
		return err
	})
	return accounts, err
}

// RecordAccountSync stores one account's sync outcome. A failure keeps its cached
// tasks and its last success time.
func (s *Store) RecordAccountSync(ctx context.Context, id int64, syncErr error, needsReconnect bool) error {
	return s.run(ctx, func(ctx context.Context) error {
		if syncErr == nil {
			_, err := s.pool.Exec(ctx, `UPDATE google_accounts SET last_success_at = now(), last_error = NULL,
				needs_reconnect = false, updated_at = now() WHERE id = $1`, id)
			return err
		}
		_, err := s.pool.Exec(ctx, `UPDATE google_accounts SET last_error = $2, needs_reconnect = $3,
			updated_at = now() WHERE id = $1`, id, syncErr.Error(), needsReconnect)
		return err
	})
}

// DeleteGoogleAccount removes an account and, by cascade, its cached tasks.
func (s *Store) DeleteGoogleAccount(ctx context.Context, id int64) error {
	return s.run(ctx, func(ctx context.Context) error {
		tag, err := s.pool.Exec(ctx, `DELETE FROM google_accounts WHERE id = $1`, id)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
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
