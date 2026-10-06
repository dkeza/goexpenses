-- Every change to a post keeps its previous version, so old values and the
-- time of each change can be looked up. Deleting a post from posts (when its
-- account is deleted) also removes its history.
CREATE TABLE posts_history
(
  id BIGSERIAL PRIMARY KEY,
  posts_id integer NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  operation character varying(10) NOT NULL CHECK (operation IN ('update', 'delete')),
  changed_at timestamp with time zone NOT NULL DEFAULT NOW(),
  description character varying NOT NULL,
  expenses_id integer NOT NULL,
  incomes_id integer NOT NULL,
  created_at timestamp NOT NULL,
  amount decimal(12,2) NOT NULL,
  accounts_id integer NOT NULL,
  exchange decimal(12,4) NOT NULL,
  deleted integer NOT NULL,
  p_id character varying NOT NULL,
  created_ts timestamp NOT NULL
);

CREATE INDEX posts_history_post_changed_idx ON posts_history (posts_id, changed_at DESC, id DESC);

CREATE FUNCTION posts_history_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO posts_history (
    posts_id, operation, description, expenses_id, incomes_id, created_at,
    amount, accounts_id, exchange, deleted, p_id, created_ts
  ) VALUES (
    OLD.id,
    CASE WHEN OLD.deleted = 0 AND NEW.deleted <> 0 THEN 'delete' ELSE 'update' END,
    OLD.description, OLD.expenses_id, OLD.incomes_id, OLD.created_at,
    OLD.amount, OLD.accounts_id, OLD.exchange, OLD.deleted, OLD.p_id, OLD.created_ts
  );
  RETURN NULL;
END;
$$;

CREATE TRIGGER posts_history_trigger
  AFTER UPDATE ON posts
  FOR EACH ROW
  WHEN (OLD.* IS DISTINCT FROM NEW.*)
  EXECUTE FUNCTION posts_history_record();
