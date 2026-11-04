-- +goose Up
CREATE TABLE google_accounts (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    google_sub          TEXT NOT NULL UNIQUE, -- Google's stable account ID; emails can change.
    email               TEXT NOT NULL,
    refresh_token_enc   TEXT NOT NULL,        -- AES-256-GCM, key in GOOGLE_TOKEN_KEY.
    last_success_at     TIMESTAMPTZ,
    last_error          TEXT,
    needs_reconnect     BOOLEAN NOT NULL DEFAULT false,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- tasks is a cache of Google's data, so it's safe to empty: the next sync refills it.
DELETE FROM tasks;
ALTER TABLE tasks DROP CONSTRAINT tasks_pkey;
ALTER TABLE tasks ADD COLUMN account_id BIGINT NOT NULL REFERENCES google_accounts (id) ON DELETE CASCADE;
ALTER TABLE tasks ADD PRIMARY KEY (account_id, google_id);

-- +goose Down
DELETE FROM tasks;
ALTER TABLE tasks DROP CONSTRAINT tasks_pkey;
ALTER TABLE tasks DROP COLUMN account_id;
ALTER TABLE tasks ADD PRIMARY KEY (google_id);
DROP TABLE google_accounts;
