package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"streaks/internal/streak"
)

// Habit is a habit with its schedule history and check-ins.
type Habit struct {
	ID         int64
	Name       string
	Source     string
	ArchivedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Schedules  []streak.Schedule
	CheckIns   []streak.Day
}

const habitColumns = `id, name, source, archived_at, created_at, updated_at`

func scanHabit(row pgx.Row) (Habit, error) {
	var h Habit
	err := row.Scan(&h.ID, &h.Name, &h.Source, &h.ArchivedAt, &h.CreatedAt, &h.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return h, ErrNotFound
	}
	return h, err
}

// ListHabits returns a user's habits, archived included, with schedules and check-ins.
func (s *Store) ListHabits(ctx context.Context, userID int64) ([]Habit, error) {
	var habits []Habit
	err := s.run(ctx, func(ctx context.Context) error {
		rows, err := s.pool.Query(ctx, `SELECT `+habitColumns+` FROM habits WHERE user_id = $1 ORDER BY created_at, id`, userID)
		if err != nil {
			return err
		}
		habits, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Habit, error) { return scanHabit(r) })
		if err != nil {
			return err
		}
		byID := make(map[int64]*Habit, len(habits))
		for i := range habits {
			byID[habits[i].ID] = &habits[i]
		}

		rows, err = s.pool.Query(ctx, `SELECT sc.habit_id, sc.effective_from, sc.days_mask FROM habit_schedules sc
			JOIN habits h ON h.id = sc.habit_id WHERE h.user_id = $1`, userID)
		if err != nil {
			return err
		}
		var id int64
		var sc streak.Schedule
		var mask int16
		if _, err := pgx.ForEachRow(rows, []any{&id, &sc.From, &mask}, func() error {
			if h := byID[id]; h != nil {
				sc.Mask = uint8(mask)
				h.Schedules = append(h.Schedules, sc)
			}
			return nil
		}); err != nil {
			return err
		}

		rows, err = s.pool.Query(ctx, `SELECT c.habit_id, c.day FROM check_ins c
			JOIN habits h ON h.id = c.habit_id WHERE h.user_id = $1 ORDER BY c.day`, userID)
		if err != nil {
			return err
		}
		var day streak.Day
		_, err = pgx.ForEachRow(rows, []any{&id, &day}, func() error {
			if h := byID[id]; h != nil {
				h.CheckIns = append(h.CheckIns, day)
			}
			return nil
		})
		return err
	})
	return habits, err
}

// GetHabit returns one of a user's habits. Another user's habit is ErrNotFound.
func (s *Store) GetHabit(ctx context.Context, userID, id int64) (Habit, error) {
	var h Habit
	err := s.run(ctx, func(ctx context.Context) error {
		var err error
		h, err = scanHabit(s.pool.QueryRow(ctx, `SELECT `+habitColumns+` FROM habits WHERE id = $1 AND user_id = $2`, id, userID))
		if err != nil {
			return err
		}
		rows, err := s.pool.Query(ctx,
			`SELECT effective_from, days_mask FROM habit_schedules WHERE habit_id = $1`, id)
		if err != nil {
			return err
		}
		h.Schedules = nil
		var sc streak.Schedule
		var mask int16
		if _, err := pgx.ForEachRow(rows, []any{&sc.From, &mask}, func() error {
			sc.Mask = uint8(mask)
			h.Schedules = append(h.Schedules, sc)
			return nil
		}); err != nil {
			return err
		}
		rows, err = s.pool.Query(ctx, `SELECT day FROM check_ins WHERE habit_id = $1 ORDER BY day`, id)
		if err != nil {
			return err
		}
		h.CheckIns, err = pgx.CollectRows(rows, pgx.RowTo[streak.Day])
		return err
	})
	return h, err
}

// CreateHabit inserts a habit and its first schedule. A retried create could
// only duplicate if the commit succeeded but its acknowledgement was lost, and
// a stray duplicate habit is easy to delete. A new GitHub habit immediately
// picks up activity that has already been synced.
func (s *Store) CreateHabit(ctx context.Context, userID int64, name, source string, mask uint8, today streak.Day) (int64, error) {
	var id int64
	err := s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO habits (user_id, name, source) VALUES ($1, $2, $3) RETURNING id`, userID, name, source).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO habit_schedules (habit_id, effective_from, days_mask) VALUES ($1, $2::date, $3)`,
			id, today, int16(mask)); err != nil {
			return err
		}
		if source == "github" {
			return applyGitHubCheckIns(ctx, tx, userID)
		}
		return nil
	})
	return id, err
}

// UpdateHabit changes the name and, when mask is non-nil, schedules the new
// target days from today onward. Earlier days keep the schedule they had.
func (s *Store) UpdateHabit(ctx context.Context, userID, id int64, name *string, mask *uint8, today streak.Day) error {
	return s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE habits SET name = COALESCE($3, name), updated_at = now() WHERE id = $1 AND user_id = $2`, id, userID, name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if mask == nil {
			return nil
		}
		var current int16
		err = tx.QueryRow(ctx, `SELECT days_mask FROM habit_schedules WHERE habit_id = $1
			ORDER BY effective_from DESC LIMIT 1`, id).Scan(&current)
		if err != nil {
			return err
		}
		if uint8(current) == *mask {
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO habit_schedules (habit_id, effective_from, days_mask)
			VALUES ($1, $2::date, $3)
			ON CONFLICT (habit_id, effective_from) DO UPDATE SET days_mask = EXCLUDED.days_mask`,
			id, today, int16(*mask))
		return err
	})
}

// DeleteHabit removes a habit and, by cascade, its history.
func (s *Store) DeleteHabit(ctx context.Context, userID, id int64) error {
	return s.run(ctx, func(ctx context.Context) error {
		tag, err := s.pool.Exec(ctx, `DELETE FROM habits WHERE id = $1 AND user_id = $2`, id, userID)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
}

// ArchiveHabit stops scheduling a habit. If today is already checked in, it
// stops from tomorrow so today's check-in still counts.
func (s *Store) ArchiveHabit(ctx context.Context, userID, id int64, today streak.Day) error {
	return s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE habits SET archived_at = COALESCE(archived_at, now()), updated_at = now()
			WHERE id = $1 AND user_id = $2`, id, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		from := today
		var doneToday bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM check_ins WHERE habit_id = $1 AND day = $2::date)`,
			id, today).Scan(&doneToday); err != nil {
			return err
		}
		if doneToday {
			from = today + 1
		}
		_, err = tx.Exec(ctx, `INSERT INTO habit_schedules (habit_id, effective_from, days_mask)
			VALUES ($1, $2::date, 0)
			ON CONFLICT (habit_id, effective_from) DO UPDATE SET days_mask = 0`, id, from)
		return err
	})
}

// UnarchiveHabit schedules the last non-archived target days from today onward.
func (s *Store) UnarchiveHabit(ctx context.Context, userID, id int64, today streak.Day) error {
	return s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE habits SET archived_at = NULL, updated_at = now() WHERE id = $1 AND user_id = $2`, id, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		// Drops an archive row dated tomorrow, or today's if it's about to be replaced.
		if _, err := tx.Exec(ctx, `DELETE FROM habit_schedules
			WHERE habit_id = $1 AND effective_from >= $2::date AND days_mask = 0`, id, today); err != nil {
			return err
		}
		var latest int16
		err = tx.QueryRow(ctx, `SELECT days_mask FROM habit_schedules WHERE habit_id = $1
			ORDER BY effective_from DESC LIMIT 1`, id).Scan(&latest)
		if errors.Is(err, pgx.ErrNoRows) {
			// Created and archived on the same day: nothing is left to restore from.
			_, err = tx.Exec(ctx, `INSERT INTO habit_schedules (habit_id, effective_from, days_mask)
				VALUES ($1, $2::date, 127)`, id, today)
			return err
		}
		if err != nil || latest != 0 {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO habit_schedules (habit_id, effective_from, days_mask)
			SELECT $1, $2::date, COALESCE((SELECT days_mask FROM habit_schedules
				WHERE habit_id = $1 AND days_mask <> 0
				ORDER BY effective_from DESC LIMIT 1), 127)
			ON CONFLICT (habit_id, effective_from) DO UPDATE SET days_mask = EXCLUDED.days_mask`, id, today)
		return err
	})
}

// PutCheckIn marks a day done. Repeating it changes nothing.
func (s *Store) PutCheckIn(ctx context.Context, userID, id int64, day streak.Day) error {
	return s.run(ctx, func(ctx context.Context) error {
		_, err := s.pool.Exec(ctx, `INSERT INTO check_ins (habit_id, day)
			SELECT id, $2::date FROM habits WHERE id = $1 AND user_id = $3
			ON CONFLICT (habit_id, day) DO NOTHING`, id, day, userID)
		return err
	})
}

// DeleteCheckIn undoes a check-in. Repeating it changes nothing.
func (s *Store) DeleteCheckIn(ctx context.Context, userID, id int64, day streak.Day) error {
	return s.run(ctx, func(ctx context.Context) error {
		_, err := s.pool.Exec(ctx, `DELETE FROM check_ins c USING habits h
			WHERE c.habit_id = h.id AND h.id = $1 AND c.day = $2::date AND h.user_id = $3`, id, day, userID)
		return err
	})
}
