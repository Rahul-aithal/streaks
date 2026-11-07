-- +goose Up
-- A GitHub activity habit counts any push or opened PR, so a second one would
-- just repeat the first. Keep each user's oldest and archive the rest, which
-- keeps their history and lets them be swapped back in.
WITH extra AS (
    SELECT id FROM (
        SELECT id, row_number() OVER (PARTITION BY user_id ORDER BY created_at, id) AS n
        FROM habits WHERE source = 'github' AND archived_at IS NULL
    ) ranked WHERE n > 1
), archived AS (
    UPDATE habits SET archived_at = now(), updated_at = now()
    WHERE id IN (SELECT id FROM extra) RETURNING id
), today AS (
    SELECT (now() AT TIME ZONE 'Asia/Kolkata')::date AS day
)
-- Like archiving in the app: a habit done today stays scheduled until tomorrow.
INSERT INTO habit_schedules (habit_id, effective_from, days_mask)
SELECT a.id,
       t.day + CASE WHEN EXISTS (SELECT 1 FROM check_ins c WHERE c.habit_id = a.id AND c.day = t.day) THEN 1 ELSE 0 END,
       0
FROM archived a CROSS JOIN today t
ON CONFLICT (habit_id, effective_from) DO UPDATE SET days_mask = 0;

CREATE UNIQUE INDEX habits_one_github_per_user ON habits (user_id)
    WHERE source = 'github' AND archived_at IS NULL;

-- +goose Down
DROP INDEX habits_one_github_per_user;
