-- +goose Up
-- Streaks now asks for read-write Tasks access so it can tick tasks off. A
-- token granted before that (or with the box unticked) can still sync, so it's
-- flagged rather than treated as needing a reconnect. Each sync refreshes it.
ALTER TABLE users ADD COLUMN google_read_only BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE users DROP COLUMN google_read_only;
