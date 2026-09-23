ALTER TABLE users
  ADD COLUMN email_verified boolean NOT NULL DEFAULT true,
  ADD COLUMN verification_token character varying,
  ADD COLUMN verification_sent_at timestamp;

CREATE UNIQUE INDEX users_verification_token_uidx ON users (verification_token)
  WHERE verification_token IS NOT NULL;
