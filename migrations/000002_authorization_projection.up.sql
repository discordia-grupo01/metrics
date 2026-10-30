CREATE TABLE metric_servers (
    server_id UUID PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE metric_roles (
    server_id UUID NOT NULL,
    role_id UUID NOT NULL,
    is_admin BOOLEAN NOT NULL DEFAULT FALSE,
    is_everyone BOOLEAN NOT NULL DEFAULT FALSE,
    occurred_at TIMESTAMPTZ NOT NULL,
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (server_id, role_id)
);

CREATE TABLE metric_member_roles (
    server_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role_ids UUID[] NOT NULL DEFAULT '{}',
    occurred_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (server_id, user_id)
);

CREATE TABLE metric_owners (
    server_id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX metric_member_roles_user ON metric_member_roles (server_id, user_id);
CREATE INDEX metric_roles_admin ON metric_roles (server_id, role_id) WHERE is_admin AND NOT deleted;
