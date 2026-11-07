package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// User is a Google account that has signed in.
type User struct {
	ID              int64
	GoogleSub       string
	Email           string
	Name            string
	AvatarURL       string
	RefreshTokenEnc string
	NeedsReconnect  bool
	GitHubLogin     *string
	GitHubTokenEnc  *string
	EmailReminders  bool
	CreatedAt       time.Time
}

const userColumns = `u.id, u.google_sub, u.email, u.name, u.avatar_url, u.refresh_token_enc,
	u.needs_reconnect, u.github_login, u.github_token_enc, u.email_reminders, u.created_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.GoogleSub, &u.Email, &u.Name, &u.AvatarURL, &u.RefreshTokenEnc,
		&u.NeedsReconnect, &u.GitHubLogin, &u.GitHubTokenEnc, &u.EmailReminders, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func collectUsers(rows pgx.Rows) ([]User, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (User, error) { return scanUser(r) })
}

// GetUserBySub finds a user by Google's stable account ID.
func (s *Store) GetUserBySub(ctx context.Context, sub string) (User, error) {
	var u User
	err := s.run(ctx, func(ctx context.Context) error {
		var err error
		u, err = scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users u WHERE u.google_sub = $1`, sub))
		return err
	})
	return u, err
}

// GetUser finds a user by ID.
func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.run(ctx, func(ctx context.Context) error {
		var err error
		u, err = scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users u WHERE u.id = $1`, id))
		return err
	})
	return u, err
}

// ListUsers returns every user, for background syncs.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	var users []User
	err := s.run(ctx, func(ctx context.Context) error {
		rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users u ORDER BY u.created_at, u.id`)
		if err != nil {
			return err
		}
		users, err = collectUsers(rows)
		return err
	})
	return users, err
}

// SaveSignIn creates or updates a user. An empty refreshTokenEnc keeps the stored
// token. A user signing in also claims any habits left unowned by the move from
// a single-user database.
func (s *Store) SaveSignIn(ctx context.Context, sub, email, name, avatarURL, refreshTokenEnc string) (int64, error) {
	var id int64
	err := s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO users (google_sub, email, name, avatar_url, refresh_token_enc)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (google_sub) DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name,
				avatar_url = EXCLUDED.avatar_url,
				refresh_token_enc = COALESCE(NULLIF(EXCLUDED.refresh_token_enc, ''), users.refresh_token_enc),
				needs_reconnect = CASE WHEN EXCLUDED.refresh_token_enc = '' THEN users.needs_reconnect ELSE false END,
				updated_at = now()
			RETURNING id`, sub, email, name, avatarURL, refreshTokenEnc).Scan(&id)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE habits SET user_id = $1 WHERE user_id IS NULL`, id)
		return err
	})
	return id, err
}

// SetNeedsReconnect flags whether Google still accepts the user's token.
func (s *Store) SetNeedsReconnect(ctx context.Context, userID int64, needed bool) error {
	return s.run(ctx, func(ctx context.Context) error {
		_, err := s.pool.Exec(ctx, `UPDATE users SET needs_reconnect = $2, updated_at = now()
			WHERE id = $1 AND needs_reconnect <> $2`, userID, needed)
		return err
	})
}

// SetGitHub links a GitHub account. Its events and check-ins are kept across
// token changes because they're keyed to the Streaks user.
func (s *Store) SetGitHub(ctx context.Context, userID int64, login, tokenEnc string) error {
	return s.run(ctx, func(ctx context.Context) error {
		_, err := s.pool.Exec(ctx, `UPDATE users SET github_login = $2, github_token_enc = $3, updated_at = now()
			WHERE id = $1`, userID, login, tokenEnc)
		return err
	})
}

// ClearGitHub unlinks GitHub. Past activity and check-ins stay.
func (s *Store) ClearGitHub(ctx context.Context, userID int64) error {
	return s.run(ctx, func(ctx context.Context) error {
		if _, err := s.pool.Exec(ctx, `UPDATE users SET github_login = NULL, github_token_enc = NULL,
			updated_at = now() WHERE id = $1`, userID); err != nil {
			return err
		}
		_, err := s.pool.Exec(ctx, `DELETE FROM sync_runs WHERE user_id = $1 AND kind = 'github'`, userID)
		return err
	})
}

// DeleteUser removes a user and, by cascade, everything they own.
func (s *Store) DeleteUser(ctx context.Context, userID int64) error {
	return s.run(ctx, func(ctx context.Context) error {
		tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
}

// --- Sessions ---

// SignInToSession adds a user to a browser session and makes them active. It
// creates the session if tokenHash is new.
func (s *Store) SignInToSession(ctx context.Context, tokenHash string, userID int64, expires time.Time) error {
	return s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// Expired sessions are only ever cleaned up here: a good moment because a
		// sign-in has already woken the database.
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sessions (token_hash, active_user_id, expires_at)
			VALUES ($1, $2, $3)
			ON CONFLICT (token_hash) DO UPDATE SET active_user_id = EXCLUDED.active_user_id,
				expires_at = EXCLUDED.expires_at`, tokenHash, userID, expires); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO session_users (token_hash, user_id) VALUES ($1, $2)
			ON CONFLICT (token_hash, user_id) DO NOTHING`, tokenHash, userID)
		return err
	})
}

// SessionUser returns a live session's active user, or ErrNotFound.
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (User, error) {
	var u User
	err := s.run(ctx, func(ctx context.Context) error {
		var err error
		u, err = scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM sessions se
			JOIN users u ON u.id = se.active_user_id
			WHERE se.token_hash = $1 AND se.expires_at > now()`, tokenHash))
		return err
	})
	return u, err
}

// SessionAccounts returns every account signed in to a session.
func (s *Store) SessionAccounts(ctx context.Context, tokenHash string) ([]User, error) {
	var users []User
	err := s.run(ctx, func(ctx context.Context) error {
		rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM session_users su
			JOIN users u ON u.id = su.user_id
			WHERE su.token_hash = $1 ORDER BY su.signed_in_at, u.id`, tokenHash)
		if err != nil {
			return err
		}
		users, err = collectUsers(rows)
		return err
	})
	return users, err
}

// SwitchSessionUser activates another account, only if it's signed in to this session.
func (s *Store) SwitchSessionUser(ctx context.Context, tokenHash string, userID int64) error {
	return s.run(ctx, func(ctx context.Context) error {
		tag, err := s.pool.Exec(ctx, `UPDATE sessions SET active_user_id = $2
			WHERE token_hash = $1 AND EXISTS (
				SELECT 1 FROM session_users WHERE token_hash = $1 AND user_id = $2)`, tokenHash, userID)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
}

// SignOutUser removes one account from a session and activates the next one. It
// reports whether any account remains; if not, the session is deleted.
func (s *Store) SignOutUser(ctx context.Context, tokenHash string, userID int64) (remaining bool, err error) {
	err = s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM session_users WHERE token_hash = $1 AND user_id = $2`,
			tokenHash, userID); err != nil {
			return err
		}
		var next int64
		err := tx.QueryRow(ctx, `SELECT user_id FROM session_users WHERE token_hash = $1
			ORDER BY signed_in_at DESC, user_id LIMIT 1`, tokenHash).Scan(&next)
		if errors.Is(err, pgx.ErrNoRows) {
			remaining = false
			_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
			return err
		}
		if err != nil {
			return err
		}
		remaining = true
		_, err = tx.Exec(ctx, `UPDATE sessions SET active_user_id = $2 WHERE token_hash = $1`, tokenHash, next)
		return err
	})
	return remaining, err
}

// DeleteSession signs every account out of a session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	return s.run(ctx, func(ctx context.Context) error {
		_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
		return err
	})
}
