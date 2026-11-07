package store

import (
	"context"

	"streaks/internal/streak"
)

// keepReminderDays is how long sent-reminder rows are kept. Only today's matter.
const keepReminderDays = 7

// SetEmailReminders turns a user's streak emails on or off.
func (s *Store) SetEmailReminders(ctx context.Context, userID int64, enabled bool) error {
	return s.run(ctx, func(ctx context.Context) error {
		_, err := s.pool.Exec(ctx, `UPDATE users SET email_reminders = $2, updated_at = now() WHERE id = $1`,
			userID, enabled)
		return err
	})
}

// ClaimReminder records that a reminder is being sent. It returns false if it
// was already claimed, so each one goes out at most once.
func (s *Store) ClaimReminder(ctx context.Context, userID int64, day streak.Day, slot string) (bool, error) {
	claimed := false
	err := s.run(ctx, func(ctx context.Context) error {
		if _, err := s.pool.Exec(ctx, `DELETE FROM reminder_sends WHERE day < $1`, day-keepReminderDays); err != nil {
			return err
		}
		tag, err := s.pool.Exec(ctx, `INSERT INTO reminder_sends (user_id, day, slot) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, userID, day, slot)
		claimed = tag.RowsAffected() == 1
		return err
	})
	return claimed, err
}

// ReleaseReminder undoes a claim after a failed send, so it can be retried.
func (s *Store) ReleaseReminder(ctx context.Context, userID int64, day streak.Day, slot string) error {
	return s.run(ctx, func(ctx context.Context) error {
		_, err := s.pool.Exec(ctx, `DELETE FROM reminder_sends WHERE user_id = $1 AND day = $2 AND slot = $3`,
			userID, day, slot)
		return err
	})
}
