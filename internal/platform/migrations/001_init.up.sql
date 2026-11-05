CREATE TABLE IF NOT EXISTS schema_migrations (
    version integer PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email text NOT NULL UNIQUE,
    display_name text NOT NULL,
    password_hash text NOT NULL,
    role text NOT NULL CHECK (role IN ('student', 'admin')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);

CREATE TABLE plugin_versions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL,
    version text NOT NULL,
    image_ref text NOT NULL,
    config_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
    enabled boolean NOT NULL DEFAULT true,
    UNIQUE (name, version)
);
INSERT INTO plugin_versions (name, version, image_ref, config_schema)
VALUES ('example-check', '0.1.0', 'birkenwald-example:0.1.0', '{"type":"object"}');

CREATE TABLE campaigns (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'open', 'closed')),
    opens_at timestamptz NOT NULL,
    closes_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (opens_at < closes_at)
);

CREATE TABLE tasks (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    campaign_id bigint NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'closed')),
    capacity integer CHECK (capacity IS NULL OR capacity > 0),
    selected_count integer NOT NULL DEFAULT 0 CHECK (selected_count >= 0),
    deadline_at timestamptz NOT NULL,
    plugin_version_id bigint NOT NULL REFERENCES plugin_versions(id),
    plugin_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, campaign_id),
    CHECK (capacity IS NULL OR selected_count <= capacity)
);
CREATE INDEX tasks_campaign_id_idx ON tasks(campaign_id);

CREATE TABLE selections (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    campaign_id bigint NOT NULL REFERENCES campaigns(id),
    student_id bigint NOT NULL REFERENCES users(id),
    task_id bigint NOT NULL,
    current_submission_id bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (campaign_id, student_id),
    FOREIGN KEY (task_id, campaign_id) REFERENCES tasks(id, campaign_id)
);
CREATE INDEX selections_student_id_idx ON selections(student_id);

CREATE TABLE files (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id bigint NOT NULL REFERENCES users(id),
    kind text NOT NULL CHECK (kind IN ('resume', 'package')),
    object_key text NOT NULL UNIQUE,
    original_name text NOT NULL,
    sha256 text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE submissions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    selection_id bigint NOT NULL REFERENCES selections(id),
    version integer NOT NULL CHECK (version > 0),
    resume_file_id bigint NOT NULL REFERENCES files(id),
    package_file_id bigint NOT NULL REFERENCES files(id),
    idempotency_key text NOT NULL,
    status text NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted', 'evaluating', 'evaluated', 'failed')),
    submitted_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (selection_id, version),
    UNIQUE (selection_id, idempotency_key),
    UNIQUE (id, selection_id)
);
ALTER TABLE selections ADD CONSTRAINT selections_current_submission_fk
    FOREIGN KEY (current_submission_id, id) REFERENCES submissions(id, selection_id);

CREATE TABLE evaluation_jobs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    submission_id bigint NOT NULL UNIQUE REFERENCES submissions(id),
    plugin_version_id bigint NOT NULL REFERENCES plugin_versions(id),
    plugin_config_snapshot jsonb NOT NULL,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    attempt integer NOT NULL DEFAULT 0,
    lease_token text,
    lease_until timestamptz,
    error_text text,
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);
CREATE INDEX evaluation_jobs_ready_idx ON evaluation_jobs(status, lease_until, id);

CREATE TABLE evaluation_results (
    job_id bigint PRIMARY KEY REFERENCES evaluation_jobs(id),
    score numeric(5,2) NOT NULL CHECK (score >= 0 AND score <= 100),
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id bigint REFERENCES users(id),
    action text NOT NULL,
    target_type text NOT NULL,
    target_id bigint NOT NULL,
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
