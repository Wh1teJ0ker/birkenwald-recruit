ALTER TABLE users ADD COLUMN disabled boolean NOT NULL DEFAULT false;

CREATE TABLE app_settings (
    key text PRIMARY KEY,
    value text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO app_settings(key, value) VALUES
    ('site_name', 'Birkenwald'),
    ('season_year', '2026'),
    ('registration_enabled', 'true');
