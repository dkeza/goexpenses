-- Owners invite members by e-mail. An invitation waits until the user with
-- that address accepts or declines it, so inviting does not reveal whether
-- the address is registered.
CREATE TABLE account_invitations
(
  id SERIAL PRIMARY KEY,
  accounts_id integer NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  email character varying NOT NULL,
  role character varying(10) NOT NULL
    CONSTRAINT account_invitations_role_check CHECK (role IN ('owner', 'editor', 'viewer')),
  -- Deleting the inviter withdraws the invitation, as removing them does.
  invited_by integer REFERENCES users(id) ON DELETE CASCADE,
  created_at timestamp with time zone NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX account_invitations_account_email_uidx ON account_invitations (accounts_id, email);
CREATE INDEX account_invitations_email_idx ON account_invitations (email);
