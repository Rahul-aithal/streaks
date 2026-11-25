package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"streaks/internal/streak"
)

// Reward rules. Each completed Google Task earns a point, up to a daily cap so
// that splitting work into tiny tasks can't farm them.
const (
	PointsPerDayCap = 5
	CoolOffCost     = 3
	SaveCost        = 2
)

var (
	ErrNotEnoughPoints = errors.New("not enough points")
	ErrAlreadySpent    = errors.New("already used for that day")
)

// Wallet is a user's points.
type Wallet struct {
	Earned      int `json:"earned"`
	Spent       int `json:"spent"`
	Balance     int `json:"balance"`
	EarnedToday int `json:"earned_today"`
}

// RewardEvent is one line of the points history.
type RewardEvent struct {
	Kind      string     `json:"kind"` // "earned", "cool_off" or "habit_save".
	Day       streak.Day `json:"day"`
	Title     string     `json:"title"` // The task, or the saved habit's name.
	Points    int        `json:"points"`
	CreatedAt time.Time  `json:"created_at"`
}

// earnedSQL sums completions per day, capped. $1 is the user, $2 the cap.
const earnedSQL = `SELECT COALESCE(sum(least(n, $2)), 0)::int FROM (
	SELECT count(*) AS n FROM task_completions WHERE user_id = $1 GROUP BY day) per_day`

const spentSQL = `SELECT COALESCE(sum(cost), 0)::int FROM reward_spends WHERE user_id = $1`

// GetWallet returns a user's points.
func (s *Store) GetWallet(ctx context.Context, userID int64, today streak.Day) (Wallet, error) {
	var w Wallet
	err := s.run(ctx, func(ctx context.Context) error {
		return s.pool.QueryRow(ctx, `SELECT (`+earnedSQL+`), (`+spentSQL+`),
			least((SELECT count(*) FROM task_completions WHERE user_id = $1 AND day = $3::date), $2)::int`,
			userID, PointsPerDayCap, today).Scan(&w.Earned, &w.Spent, &w.EarnedToday)
	})
	w.Balance = w.Earned - w.Spent
	return w, err
}

// RewardHistory returns the newest earnings and spends. A task beyond its day's
// cap shows with 0 points.
func (s *Store) RewardHistory(ctx context.Context, userID int64, limit int) ([]RewardEvent, error) {
	var events []RewardEvent
	err := s.run(ctx, func(ctx context.Context) error {
		rows, err := s.pool.Query(ctx, `
			SELECT 'earned', day, title,
				CASE WHEN row_number() OVER (PARTITION BY day ORDER BY earned_at, google_id) <= $2 THEN 1 ELSE 0 END,
				earned_at
			FROM task_completions WHERE user_id = $1
			UNION ALL
			SELECT kind, day, COALESCE(habit_name, ''), -cost, created_at
			FROM reward_spends WHERE user_id = $1
			ORDER BY 5 DESC LIMIT $3`, userID, PointsPerDayCap, limit)
		if err != nil {
			return err
		}
		events, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (RewardEvent, error) {
			var e RewardEvent
			err := r.Scan(&e.Kind, &e.Day, &e.Title, &e.Points, &e.CreatedAt)
			return e, err
		})
		return err
	})
	return events, err
}

// CoolOffDays returns a user's cool-off days from a day onward.
func (s *Store) CoolOffDays(ctx context.Context, userID int64, from streak.Day) ([]streak.Day, error) {
	var days []streak.Day
	err := s.run(ctx, func(ctx context.Context) error {
		rows, err := s.pool.Query(ctx, `SELECT day FROM reward_spends
			WHERE user_id = $1 AND kind = 'cool_off' AND day >= $2::date ORDER BY day`, userID, from)
		if err != nil {
			return err
		}
		days, err = pgx.CollectRows(rows, pgx.RowTo[streak.Day])
		return err
	})
	return days, err
}

// SpendCoolOff buys a cool-off day.
func (s *Store) SpendCoolOff(ctx context.Context, userID int64, day streak.Day) error {
	return s.spend(ctx, userID, CoolOffCost, func(ctx context.Context, tx pgx.Tx) (bool, error) {
		tag, err := tx.Exec(ctx, `INSERT INTO reward_spends (user_id, kind, day, cost)
			VALUES ($1, 'cool_off', $2::date, $3) ON CONFLICT DO NOTHING`, userID, day, CoolOffCost)
		return tag.RowsAffected() == 1, err
	})
}

// SpendSave marks one of the user's habits done on a day.
func (s *Store) SpendSave(ctx context.Context, userID, habitID int64, day streak.Day) error {
	return s.spend(ctx, userID, SaveCost, func(ctx context.Context, tx pgx.Tx) (bool, error) {
		tag, err := tx.Exec(ctx, `INSERT INTO reward_spends (user_id, kind, day, habit_id, habit_name, cost)
			SELECT $1, 'habit_save', $3::date, id, name, $4 FROM habits WHERE id = $2 AND user_id = $1
			ON CONFLICT DO NOTHING`, userID, habitID, day, SaveCost)
		return tag.RowsAffected() == 1, err
	})
}

// spend checks the balance and runs insert in one transaction. Locking the
// user's row serializes spends, so two at once can't overdraw.
func (s *Store) spend(ctx context.Context, userID int64, cost int, insert func(context.Context, pgx.Tx) (bool, error)) error {
	return s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var balance int
		err := tx.QueryRow(ctx, `SELECT (`+earnedSQL+`) - (`+spentSQL+`)
			FROM users WHERE id = $1 FOR UPDATE`, userID, PointsPerDayCap).Scan(&balance)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if balance < cost {
			return ErrNotEnoughPoints
		}
		inserted, err := insert(ctx, tx)
		if err == nil && !inserted {
			return ErrAlreadySpent
		}
		return err
	})
}

// RefundCoolOff cancels a cool-off day that hasn't started yet. Repeating it
// changes nothing.
func (s *Store) RefundCoolOff(ctx context.Context, userID int64, day, today streak.Day) error {
	return s.run(ctx, func(ctx context.Context) error {
		_, err := s.pool.Exec(ctx, `DELETE FROM reward_spends
			WHERE user_id = $1 AND kind = 'cool_off' AND day = $2::date AND day > $3::date`, userID, day, today)
		return err
	})
}
