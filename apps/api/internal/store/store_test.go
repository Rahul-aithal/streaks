package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsConnError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"unique violation", &pgconn.PgError{Code: "23505"}, false},
		{"syntax error", &pgconn.PgError{Code: "42601"}, false},
		{"cancelled statement", &pgconn.PgError{Code: "57014"}, false},
		{"admin shutdown when compute suspends", &pgconn.PgError{Code: "57P01"}, true},
		{"connection failure", &pgconn.PgError{Code: "08006"}, true},
		{"too many connections", &pgconn.PgError{Code: "53300"}, true},
		{"eof", fmt.Errorf("read: %w", io.EOF), true},
		{"attempt timed out", context.DeadlineExceeded, true},
		{"cancelled", context.Canceled, false},
		{"unrelated", errors.New("boom"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isConnError(tc.err); got != tc.want {
				t.Errorf("isConnError = %v, want %v", got, tc.want)
			}
		})
	}
}
