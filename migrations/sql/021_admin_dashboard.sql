ALTER TABLE users
  ADD COLUMN is_admin boolean NOT NULL DEFAULT false,
  ADD COLUMN blocked_at timestamp with time zone,
  ADD COLUMN blocked_reason text;

-- Existing verified account only. A fresh installation must promote its first
-- administrator explicitly after registration and e-mail verification.
UPDATE users SET is_admin = true
WHERE lower(btrim(username)) = 'keza' AND email_verified = true;

CREATE TABLE admin_events
(
  id BIGSERIAL PRIMARY KEY,
  kind character varying(40) NOT NULL,
  status character varying(30) NOT NULL,
  user_id integer,
  actor_user_id integer,
  subject character varying(254) NOT NULL DEFAULT '',
  detail text NOT NULL DEFAULT '',
  item_count integer,
  created_at timestamp with time zone NOT NULL DEFAULT NOW()
);

CREATE INDEX admin_events_created_idx ON admin_events (created_at DESC, id DESC);
CREATE INDEX admin_events_user_created_idx ON admin_events (user_id, created_at DESC);
