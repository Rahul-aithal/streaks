-- +goose Up
CREATE TABLE habits (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    source      TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'github')),
    archived_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Target days are effective-dated so that editing them, or archiving a habit,
-- never rewrites past streaks. days_mask: bit 0 = Sunday ... bit 6 = Saturday.
-- 127 = every day, 0 = nothing scheduled (archived).
CREATE TABLE habit_schedules (
    habit_id       BIGINT NOT NULL REFERENCES habits (id) ON DELETE CASCADE,
    effective_from DATE NOT NULL,
    days_mask      SMALLINT NOT NULL CHECK (days_mask BETWEEN 0 AND 127),
    PRIMARY KEY (habit_id, effective_from)
);

CREATE TABLE check_ins (
    habit_id   BIGINT NOT NULL REFERENCES habits (id) ON DELETE CASCADE,
    day        DATE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (habit_id, day)
);
CREATE INDEX check_ins_day_idx ON check_ins (day);

CREATE TABLE github_events (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    day         DATE NOT NULL
);
CREATE INDEX github_events_day_idx ON github_events (day);

CREATE TABLE tasks (
    google_id    TEXT PRIMARY KEY,
    list_id      TEXT NOT NULL,
    title        TEXT NOT NULL,
    status       TEXT NOT NULL,
    due          DATE,
    completed_at TIMESTAMPTZ,
    synced_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX tasks_due_idx ON tasks (due);

CREATE TABLE task_snapshots (
    day       DATE PRIMARY KEY,
    total     INT NOT NULL,
    completed INT NOT NULL,
    overdue   INT NOT NULL,
    taken_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sync_runs (
    kind            TEXT PRIMARY KEY,
    last_success_at TIMESTAMPTZ,
    last_error      TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE sync_runs;
DROP TABLE task_snapshots;
DROP TABLE tasks;
DROP TABLE github_events;
DROP TABLE check_ins;
DROP TABLE habit_schedules;
DROP TABLE habits;
