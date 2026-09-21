DROP INDEX posts_active_account_date_idx;

CREATE INDEX posts_active_account_date_id_idx
  ON posts (accounts_id, created_at DESC, id DESC)
  WHERE deleted = 0;
