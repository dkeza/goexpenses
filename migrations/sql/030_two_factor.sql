-- Two-factor authentication (TOTP). The secret is stored encrypted; recovery
-- codes are stored as bcrypt hashes. The second sign-in step and the setup
-- secret live in the session row, so the cookie only identifies the session.
ALTER TABLE users
  ADD COLUMN totp_secret     text,
  ADD COLUMN totp_enabled_at timestamptz,
  ADD COLUMN totp_last_step  bigint NOT NULL DEFAULT 0;

ALTER TABLE sessions
  ADD COLUMN pending_2fa_user_id     integer,
  ADD COLUMN pending_2fa_at          timestamptz,
  ADD COLUMN pending_2fa_failures    integer NOT NULL DEFAULT 0,
  ADD COLUMN pending_totp_secret     text,
  ADD COLUMN pending_totp_created_at timestamptz;

CREATE INDEX sessions_pending_2fa_user_idx
  ON sessions (pending_2fa_user_id) WHERE pending_2fa_user_id IS NOT NULL;

CREATE TABLE user_recovery_codes (
  id         SERIAL PRIMARY KEY,
  user_id    integer NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash  text NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX user_recovery_codes_user_idx
  ON user_recovery_codes (user_id) WHERE used_at IS NULL;
