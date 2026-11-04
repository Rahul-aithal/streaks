-- +goose Up
-- Every Google account is now a user who owns their own data.
ALTER TABLE google_accounts RENAME TO users;
ALTER TABLE users DROP COLUMN last_success_at;
ALTER TABLE users DROP COLUMN last_error;
ALTER TABLE users ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN avatar_url TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN github_login TEXT;
ALTER TABLE users ADD COLUMN github_token_enc TEXT; -- AES-256-GCM, like refresh_token_enc.

ALTER TABLE tasks RENAME COLUMN account_id TO user_id;

-- Existing data goes to the oldest account. If none exists yet, habits stay
-- unowned and invisible until the first sign-in claims them.
ALTER TABLE habits ADD COLUMN user_id BIGINT REFERENCES users (id) ON DELETE CASCADE;
UPDATE habits SET user_id = (SELECT id FROM users ORDER BY created_at, id LIMIT 1);
CREATE INDEX habits_user_idx ON habits (user_id);

ALTER TABLE github_events ADD COLUMN user_id BIGINT REFERENCES users (id) ON DELETE CASCADE;
UPDATE github_events SET user_id = (SELECT id FROM users ORDER BY created_at, id LIMIT 1);
DELETE FROM github_events WHERE user_id IS NULL;
ALTER TABLE github_events ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE github_events DROP CONSTRAINT github_events_pkey;
ALTER TABLE github_events ADD PRIMARY KEY (user_id, id);
DROP INDEX github_events_day_idx;
CREATE INDEX github_events_user_day_idx ON github_events (user_id, day);

ALTER TABLE task_snapshots ADD COLUMN user_id BIGINT REFERENCES users (id) ON DELETE CASCADE;
UPDATE task_snapshots SET user_id = (SELECT id FROM users ORDER BY created_at, id LIMIT 1);
DELETE FROM task_snapshots WHERE user_id IS NULL;
ALTER TABLE task_snapshots ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE task_snapshots DROP CONSTRAINT task_snapshots_pkey;
ALTER TABLE task_snapshots ADD PRIMARY KEY (user_id, day);

-- Only timestamps of past syncs: the next sync recreates these per user.
DELETE FROM sync_runs;
ALTER TABLE sync_runs ADD COLUMN user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE sync_runs DROP CONSTRAINT sync_runs_pkey;
ALTER TABLE sync_runs ADD PRIMARY KEY (user_id, kind);

-- One browser session can hold several signed-in accounts, one of them active.
CREATE TABLE sessions (
    token_hash     TEXT PRIMARY KEY, -- SHA-256 of the cookie, so a DB leak can't be replayed.
    active_user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at     TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE session_users (
    token_hash   TEXT NOT NULL REFERENCES sessions (token_hash) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    signed_in_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (token_hash, user_id)
);
CREATE INDEX session_users_user_idx ON session_users (user_id);

-- +goose Down
DROP TABLE session_users;
DROP TABLE sessions;

DELETE FROM sync_runs;
ALTER TABLE sync_runs DROP CONSTRAINT sync_runs_pkey;
ALTER TABLE sync_runs DROP COLUMN user_id;
ALTER TABLE sync_runs ADD PRIMARY KEY (kind);

-- Per-user rows can't fit the old single-user keys, and both tables are
-- rebuilt by syncing.
DELETE FROM task_snapshots;
ALTER TABLE task_snapshots DROP CONSTRAINT task_snapshots_pkey;
ALTER TABLE task_snapshots DROP COLUMN user_id;
ALTER TABLE task_snapshots ADD PRIMARY KEY (day);

DELETE FROM github_events;
DROP INDEX github_events_user_day_idx;
ALTER TABLE github_events DROP CONSTRAINT github_events_pkey;
ALTER TABLE github_events DROP COLUMN user_id;
ALTER TABLE github_events ADD PRIMARY KEY (id);
CREATE INDEX github_events_day_idx ON github_events (day);

DROP INDEX habits_user_idx;
ALTER TABLE habits DROP COLUMN user_id;

ALTER TABLE tasks RENAME COLUMN user_id TO account_id;

ALTER TABLE users DROP COLUMN github_token_enc;
ALTER TABLE users DROP COLUMN github_login;
ALTER TABLE users DROP COLUMN avatar_url;
ALTER TABLE users DROP COLUMN name;
ALTER TABLE users ADD COLUMN last_success_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN last_error TEXT;
ALTER TABLE users RENAME TO google_accounts;
