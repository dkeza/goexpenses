-- The posts list filter is saved per account as JSON so it can hold more
-- than a date range. An existing date filter is carried over; fromdate and
-- todate are kept so an older application version still finds them.
ALTER TABLE accounts ADD COLUMN post_filter character varying NOT NULL DEFAULT '';

UPDATE accounts
  SET post_filter = json_build_object('from', fromdate, 'to', todate)::text
  WHERE fromdate <> '' AND todate <> '';
