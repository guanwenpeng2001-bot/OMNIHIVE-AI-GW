CREATE TABLE registration_agreements (
    user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    revision text NOT NULL,
    accepted_at timestamptz NOT NULL DEFAULT now()
);
