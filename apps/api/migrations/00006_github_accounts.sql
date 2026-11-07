-- +goose Up
-- A user can connect several GitHub accounts. Their activity is pooled, so the
-- GitHub activity habit counts a push or PR from any of them.
CREATE TABLE github_accounts (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    login           TEXT NOT NULL,
    token_enc       TEXT NOT NULL, -- AES-256-GCM, like users.refresh_token_enc.
    last_success_at TIMESTAMPTZ,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, login)
);

INSERT INTO github_accounts (user_id, login, token_enc)
SELECT id, github_login, github_token_enc FROM users
WHERE github_login IS NOT NULL AND github_token_enc IS NOT NULL;

ALTER TABLE users DROP COLUMN github_login;
ALTER TABLE users DROP COLUMN github_token_enc;

-- +goose Down
ALTER TABLE users ADD COLUMN github_login TEXT;
ALTER TABLE users ADD COLUMN github_token_enc TEXT;
-- Only one link fits the old shape: keep each user's first.
UPDATE users u SET github_login = g.login, github_token_enc = g.token_enc
FROM (SELECT DISTINCT ON (user_id) user_id, login, token_enc FROM github_accounts ORDER BY user_id, created_at, id) g
WHERE g.user_id = u.id;
DROP TABLE github_accounts;
