UPDATE posts
SET p_id = md5('posts:' || id::text || ':' || random()::text || ':' || clock_timestamp()::text)
WHERE p_id = '';

UPDATE expenses
SET p_id = md5('expenses:' || id::text || ':' || random()::text || ':' || clock_timestamp()::text)
WHERE p_id = '';

UPDATE incomes
SET p_id = md5('incomes:' || id::text || ':' || random()::text || ':' || clock_timestamp()::text)
WHERE p_id = '';
