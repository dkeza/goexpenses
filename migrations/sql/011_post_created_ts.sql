ALTER TABLE posts
ADD COLUMN IF NOT EXISTS created_ts timestamp NOT NULL DEFAULT NOW();

UPDATE posts
SET created_ts = created_at;
