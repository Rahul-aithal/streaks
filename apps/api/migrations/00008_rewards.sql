-- +goose Up
-- Finishing a Google Task earns reward points. tasks is a cache that each sync
-- rebuilds, so earnings get their own table: one row per task, ever, so a task
-- completed, reopened and completed again earns once.
CREATE TABLE task_completions (
    user_id   BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    google_id TEXT NOT NULL,
    title     TEXT NOT NULL,
    day       DATE NOT NULL, -- IST day it was completed; the daily earning cap uses it.
    earned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, google_id)
);
CREATE INDEX task_completions_user_day_idx ON task_completions (user_id, day);

-- Points spent. A cool-off day holds every streak; a save marks one habit done
-- on one day. A save outlives its habit so deleting the habit doesn't refund it.
CREATE TABLE reward_spends (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('cool_off', 'habit_save')),
    day        DATE NOT NULL,
    habit_id   BIGINT REFERENCES habits (id) ON DELETE SET NULL,
    habit_name TEXT,
    cost       INTEGER NOT NULL CHECK (cost >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX reward_spends_user_idx ON reward_spends (user_id, day);
CREATE UNIQUE INDEX reward_spends_one_cool_off ON reward_spends (user_id, day) WHERE kind = 'cool_off';
CREATE UNIQUE INDEX reward_spends_one_save ON reward_spends (habit_id, day) WHERE kind = 'habit_save';

-- +goose Down
DROP TABLE reward_spends;
DROP TABLE task_completions;
