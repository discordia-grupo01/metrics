CREATE TABLE metric_events (
    event_id UUID PRIMARY KEY,
    event_type TEXT NOT NULL,
    server_id UUID,
    user_id UUID,
    occurred_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX metric_events_server_time ON metric_events (server_id, occurred_at);
CREATE INDEX metric_events_type_time ON metric_events (event_type, occurred_at);

CREATE TABLE incidents (
    id UUID PRIMARY KEY,
    title TEXT NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 160),
    description TEXT NOT NULL CHECK (char_length(btrim(description)) BETWEEN 1 AND 5000),
    affected_services TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'resolved')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);
