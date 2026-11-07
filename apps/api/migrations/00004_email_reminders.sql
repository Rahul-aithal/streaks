-- +goose Up
ALTER TABLE users ADD COLUMN email_reminders BOOLEAN NOT NULL DEFAULT true;

-- One row per reminder sent, so a restart or a second API never sends it twice.
CREATE TABLE reminder_sends (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day     DATE NOT NULL,
    slot    TEXT NOT NULL,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, day, slot)
);

-- +goose Down
DROP TABLE reminder_sends;
ALTER TABLE users DROP COLUMN email_reminders;
