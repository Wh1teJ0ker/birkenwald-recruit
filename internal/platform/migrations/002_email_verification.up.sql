CREATE TABLE email_verification_codes (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email text NOT NULL,
    purpose text NOT NULL DEFAULT 'register' CHECK (purpose IN ('register')),
    code_hash text NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);

-- At most one active code per email and purpose; resending invalidates the old one.
CREATE UNIQUE INDEX email_verification_codes_active_idx
    ON email_verification_codes(email, purpose)
    WHERE consumed_at IS NULL;

CREATE INDEX email_verification_codes_email_idx
    ON email_verification_codes(email, created_at DESC);
