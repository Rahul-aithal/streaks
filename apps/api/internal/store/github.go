package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// GitHubAccount is one GitHub account a user has connected.
type GitHubAccount struct {
	ID            int64
	UserID        int64
	Login         string
	TokenEnc      string
	LastSuccessAt *time.Time
	LastError     *string
	CreatedAt     time.Time
}

const githubColumns = `id, user_id, login, token_enc, last_success_at, last_error, created_at`

func scanGitHubAccount(row pgx.Row) (GitHubAccount, error) {
	var a GitHubAccount
	err := row.Scan(&a.ID, &a.UserID, &a.Login, &a.TokenEnc, &a.LastSuccessAt, &a.LastError, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// ListGitHubAccounts returns a user's connected GitHub accounts, oldest first.
func (s *Store) ListGitHubAccounts(ctx context.Context, userID int64) ([]GitHubAccount, error) {
	var accounts []GitHubAccount
	err := s.run(ctx, func(ctx context.Context) error {
		rows, err := s.pool.Query(ctx, `SELECT `+githubColumns+` FROM github_accounts
			WHERE user_id = $1 ORDER BY created_at, id`, userID)
		if err != nil {
			return err
		}
		accounts, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (GitHubAccount, error) {
			return scanGitHubAccount(r)
		})
		return err
	})
	return accounts, err
}

// SaveGitHubAccount connects a GitHub account, or replaces the token of one
// already connected. It returns the replaced token, if any, so it can be revoked.
func (s *Store) SaveGitHubAccount(ctx context.Context, userID int64, login, tokenEnc string) (replaced *string, err error) {
	err = s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		replaced = nil
		var old string
		err := tx.QueryRow(ctx, `SELECT token_enc FROM github_accounts WHERE user_id = $1 AND login = $2 FOR UPDATE`,
			userID, login).Scan(&old)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		default:
			replaced = &old
		}
		_, err = tx.Exec(ctx, `INSERT INTO github_accounts (user_id, login, token_enc) VALUES ($1, $2, $3)
			ON CONFLICT (user_id, login) DO UPDATE SET token_enc = EXCLUDED.token_enc, last_error = NULL`,
			userID, login, tokenEnc)
		return err
	})
	return replaced, err
}

// DeleteGitHubAccount disconnects one account and returns it, so its token can
// be revoked. Past activity and check-ins stay.
func (s *Store) DeleteGitHubAccount(ctx context.Context, userID, id int64) (GitHubAccount, error) {
	var a GitHubAccount
	err := s.tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		a, err = scanGitHubAccount(tx.QueryRow(ctx, `DELETE FROM github_accounts WHERE id = $1 AND user_id = $2
			RETURNING `+githubColumns, id, userID))
		if err != nil {
			return err
		}
		// Its counts go too; the check-ins they made stay.
		if _, err := tx.Exec(ctx, `DELETE FROM github_contributions WHERE user_id = $1 AND login = $2`,
			userID, a.Login); err != nil {
			return err
		}
		// With nothing left to sync, the old sync status would only mislead.
		_, err = tx.Exec(ctx, `DELETE FROM sync_runs WHERE user_id = $1 AND kind = 'github'
			AND NOT EXISTS (SELECT 1 FROM github_accounts WHERE user_id = $1)`, userID)
		return err
	})
	return a, err
}

// RecordGitHubAccountSync stores one account's sync outcome.
func (s *Store) RecordGitHubAccountSync(ctx context.Context, id int64, syncErr error) error {
	return s.run(ctx, func(ctx context.Context) error {
		if syncErr == nil {
			_, err := s.pool.Exec(ctx, `UPDATE github_accounts SET last_success_at = now(), last_error = NULL WHERE id = $1`, id)
			return err
		}
		_, err := s.pool.Exec(ctx, `UPDATE github_accounts SET last_error = $2 WHERE id = $1`, id, syncErr.Error())
		return err
	})
}
