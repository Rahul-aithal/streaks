-- +goose Up
-- GitHub activity now comes from the contribution calendar (the green graph),
-- which includes private work and a full year, instead of the events feed. The
-- next sync refills it; check-ins already made are kept.
CREATE TABLE github_contributions (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    login   TEXT NOT NULL,
    day     DATE NOT NULL,
    count   INTEGER NOT NULL CHECK (count > 0),
    PRIMARY KEY (user_id, login, day)
);
CREATE INDEX github_contributions_user_day_idx ON github_contributions (user_id, day);
DROP TABLE github_events;

-- +goose Down
CREATE TABLE github_events (
    user_id     BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    id          TEXT NOT NULL,
    type        TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    day         DATE NOT NULL,
    PRIMARY KEY (user_id, id)
);
CREATE INDEX github_events_user_day_idx ON github_events (user_id, day);
DROP TABLE github_contributions;
