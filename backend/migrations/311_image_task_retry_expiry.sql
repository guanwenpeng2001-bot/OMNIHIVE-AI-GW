-- Worker startup and retention sweep only need rows with retained retry input.
CREATE INDEX IF NOT EXISTS image_tasks_retry_input_expiry ON image_tasks (input_until)
    WHERE replay_input IS NOT NULL;
