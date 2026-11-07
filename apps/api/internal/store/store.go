// Package store holds all SQL. Every query runs through run, which applies a
// timeout and retries once on connection errors (Neon may have scaled to zero).
// Writes are upserts or idempotent deletes, so a retry can't double-apply them;
// CreateHabit is the one exception and is documented there.
package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	connectTimeout = 10 * time.Second
	queryTimeout   = 15 * time.Second // Includes waiting for a connection.
	maxConns       = 4
)

var ErrNotFound = errors.New("not found")

// ErrGitHubHabitExists means the user already has an active GitHub habit.
var ErrGitHubHabitExists = errors.New("an active GitHub habit already exists")

// githubHabitConflict turns the one-GitHub-habit index violation into
// ErrGitHubHabitExists.
func githubHabitConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "habits_one_github_per_user" {
		return ErrGitHubHabitExists
	}
	return err
}

type Store struct {
	pool *pgxpool.Pool
}

// Open creates the pool. No connection is made until the first query, so
// startup doesn't wake a suspended Neon compute.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("invalid DATABASE_URL") // The parse error can quote the password.
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 2 * time.Minute
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	// Neon's pooled endpoint is PgBouncer: don't rely on server-side prepared
	// statements surviving between transactions.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) run(ctx context.Context, fn func(context.Context) error) error {
	attempt := func() error {
		ctx, cancel := context.WithTimeout(ctx, queryTimeout)
		defer cancel()
		return fn(ctx)
	}
	err := attempt()
	if err != nil && ctx.Err() == nil && isConnError(err) {
		err = attempt()
	}
	return err
}

func (s *Store) tx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return s.run(ctx, func(ctx context.Context) error {
		return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(ctx, tx) })
	})
}

// isConnError reports errors worth one retry: the connection failed, as opposed
// to the query being wrong.
func isConnError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code[:2] {
		case "08", "53": // Connection exception; insufficient resources.
			return true
		case "57": // Operator intervention, except a query cancelled by a client.
			return pgErr.Code != "57014"
		}
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) { // Slow cold start.
		return true
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var connectErr *pgconn.ConnectError
	var netErr net.Error
	return errors.As(err, &connectErr) || errors.As(err, &netErr) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || pgconn.SafeToRetry(err)
}
