-- Table: public.accounts

DROP TABLE IF EXISTS public.accounts;

CREATE TABLE public.accounts
(
  id SERIAL PRIMARY KEY,
  description character varying NOT NULL DEFAULT ''::character varying,
  deleted boolean NOT NULL DEFAULT false,
  fromdate character varying NOT NULL DEFAULT ''::character varying,
  todate character varying NOT NULL DEFAULT ''::character varying
);

-- Table: public.users

DROP TABLE IF EXISTS public.users;

CREATE TABLE public.users
(
  id SERIAL PRIMARY KEY,
  name character varying NOT NULL DEFAULT ''::character varying,
  email character varying NOT NULL DEFAULT ''::character varying,
  password character varying NOT NULL DEFAULT ''::character varying,
  created_at timestamp NOT NULL DEFAULT NOW(),
  username character varying NOT NULL DEFAULT ''::character varying,
  default_accounts_id integer NOT NULL DEFAULT 0,
  lang character varying NOT NULL DEFAULT 'EN'::character varying
);