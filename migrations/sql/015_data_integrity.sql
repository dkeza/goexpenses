DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM users
    WHERE btrim(username) <> ''
    GROUP BY lower(btrim(username))
    HAVING COUNT(*) > 1
  ) THEN
    RAISE EXCEPTION 'cannot enforce unique usernames: case-insensitive duplicates exist';
  END IF;

  IF EXISTS (
    SELECT 1 FROM users
    WHERE btrim(email) <> ''
    GROUP BY lower(btrim(email))
    HAVING COUNT(*) > 1
  ) THEN
    RAISE EXCEPTION 'cannot enforce unique email addresses: case-insensitive duplicates exist';
  END IF;

  IF EXISTS (SELECT 1 FROM posts WHERE p_id <> '' GROUP BY p_id HAVING COUNT(*) > 1) OR
     EXISTS (SELECT 1 FROM expenses WHERE p_id <> '' GROUP BY p_id HAVING COUNT(*) > 1) OR
     EXISTS (SELECT 1 FROM incomes WHERE p_id <> '' GROUP BY p_id HAVING COUNT(*) > 1) THEN
    RAISE EXCEPTION 'cannot enforce unique public IDs: duplicates exist';
  END IF;
END $$;

DELETE FROM accountsusers duplicate
USING accountsusers original
WHERE duplicate.accounts_id = original.accounts_id
  AND duplicate.users_id = original.users_id
  AND duplicate.id > original.id;

CREATE UNIQUE INDEX users_username_lower_uidx
  ON users (lower(btrim(username)))
  WHERE btrim(username) <> '';
CREATE UNIQUE INDEX users_email_lower_uidx
  ON users (lower(btrim(email)))
  WHERE btrim(email) <> '';
CREATE UNIQUE INDEX posts_public_id_uidx ON posts (p_id) WHERE p_id <> '';
CREATE UNIQUE INDEX expenses_public_id_uidx ON expenses (p_id) WHERE p_id <> '';
CREATE UNIQUE INDEX incomes_public_id_uidx ON incomes (p_id) WHERE p_id <> '';
CREATE UNIQUE INDEX accountsusers_account_user_uidx ON accountsusers (accounts_id, users_id);

CREATE INDEX posts_active_account_date_idx
  ON posts (accounts_id, created_at DESC)
  WHERE deleted = 0;
CREATE INDEX expenses_active_account_description_idx
  ON expenses (accounts_id, description)
  WHERE deleted = 0;
CREATE INDEX incomes_active_account_description_idx
  ON incomes (accounts_id, description)
  WHERE deleted = 0;
CREATE INDEX sessions_created_at_idx ON sessions (created_at);
CREATE INDEX passwordresets_active_email_created_idx
  ON passwordresets (email, created_at DESC)
  WHERE done = 0;
CREATE INDEX accountsusers_user_idx ON accountsusers (users_id);

ALTER TABLE users
  ADD CONSTRAINT users_name_not_blank CHECK (btrim(name) <> '') NOT VALID,
  ADD CONSTRAINT users_username_not_blank CHECK (btrim(username) <> '') NOT VALID,
  ADD CONSTRAINT users_email_not_blank CHECK (btrim(email) <> '') NOT VALID,
  ADD CONSTRAINT users_default_account_fk FOREIGN KEY (default_accounts_id) REFERENCES accounts(id) NOT VALID;

ALTER TABLE posts
  ADD CONSTRAINT posts_public_id_not_blank CHECK (btrim(p_id) <> '') NOT VALID,
  ADD CONSTRAINT posts_account_fk FOREIGN KEY (accounts_id) REFERENCES accounts(id) NOT VALID;

ALTER TABLE expenses
  ADD CONSTRAINT expenses_public_id_not_blank CHECK (btrim(p_id) <> '') NOT VALID,
  ADD CONSTRAINT expenses_account_fk FOREIGN KEY (accounts_id) REFERENCES accounts(id) NOT VALID;

ALTER TABLE incomes
  ADD CONSTRAINT incomes_public_id_not_blank CHECK (btrim(p_id) <> '') NOT VALID,
  ADD CONSTRAINT incomes_account_fk FOREIGN KEY (accounts_id) REFERENCES accounts(id) NOT VALID;

ALTER TABLE accountsusers
  ADD CONSTRAINT accountsusers_account_fk FOREIGN KEY (accounts_id) REFERENCES accounts(id) NOT VALID,
  ADD CONSTRAINT accountsusers_user_fk FOREIGN KEY (users_id) REFERENCES users(id) NOT VALID;
