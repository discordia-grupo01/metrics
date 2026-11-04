CREATE TABLE staff_members (
    user_id UUID PRIMARY KEY,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
