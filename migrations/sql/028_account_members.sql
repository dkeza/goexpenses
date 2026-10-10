-- Accounts can be shared, so each member gets a role and keeps their own
-- posts list filter. Existing members own their accounts. The filter stays
-- in accounts as well, so an older application version still finds it.
ALTER TABLE accountsusers
  ADD COLUMN role character varying(10) NOT NULL DEFAULT 'owner'
    CONSTRAINT accountsusers_role_check CHECK (role IN ('owner', 'editor', 'viewer')),
  ADD COLUMN created_at timestamp with time zone NOT NULL DEFAULT NOW(),
  ADD COLUMN post_filter character varying NOT NULL DEFAULT '';

UPDATE accountsusers au
  SET post_filter = a.post_filter
  FROM accounts a
  WHERE a.id = au.accounts_id AND a.post_filter <> '';
