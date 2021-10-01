-- Table: public.accounts

DROP TABLE IF EXISTS public.accounts;

CREATE TABLE public.accounts
(
  id SERIAL PRIMARY KEY,
  description character varying NOT NULL DEFAULT ''::character varying,
  deleted integer NOT NULL DEFAULT 0,
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


-- Table: public.sessions

DROP TABLE IF EXISTS public.sessions;

CREATE TABLE public.sessions
(
  id SERIAL PRIMARY KEY,
  uuid character varying UNIQUE NOT NULL DEFAULT ''::character varying,
  user_id integer NOT NULL DEFAULT 0,
  lang character varying NOT NULL DEFAULT 'EN'::character varying,
  message character varying NOT NULL DEFAULT ''::character varying,
  expenses_id integer NOT NULL DEFAULT 0,
  last_post_description character varying NOT NULL DEFAULT ''::character varying,
  message_success integer NOT NULL DEFAULT 0,
  created_at timestamp NOT NULL DEFAULT NOW()
);

-- Table: public.posts

DROP TABLE IF EXISTS public.posts;

CREATE TABLE public.posts
(
  id SERIAL PRIMARY KEY,
  description character varying NOT NULL DEFAULT ''::character varying,
  expenses_id integer NOT NULL DEFAULT 0,
  incomes_id integer NOT NULL DEFAULT 0,
  created_at timestamp NOT NULL DEFAULT NOW(),
  amount decimal(12,2) NOT NULL DEFAULT 0,
  accounts_id integer NOT NULL DEFAULT 0,
  exchange decimal(12,4) NOT NULL DEFAULT 0,
  deleted integer NOT NULL DEFAULT 0,
  p_id character varying NOT NULL DEFAULT ''::character varying
  );

-- Table: public.currencies

DROP TABLE IF EXISTS public.currencies;

CREATE TABLE public.currencies
(
  id SERIAL PRIMARY KEY,
  code character varying UNIQUE NOT NULL DEFAULT ''::character varying,
  rate decimal(12,4) NOT NULL DEFAULT 0,
  date date
);

-- Table: public.expenses

DROP TABLE IF EXISTS public.expenses;

CREATE TABLE public.expenses
(
  id SERIAL PRIMARY KEY,
  description character varying NOT NULL DEFAULT ''::character varying,
  accounts_id integer NOT NULL DEFAULT 0,
  amount decimal(12,2) NOT NULL DEFAULT 0,
  exchange decimal(12,4) NOT NULL DEFAULT 0,
  deleted integer NOT NULL DEFAULT 0,
  expenses_id integer NOT NULL DEFAULT 0,
  p_id character varying NOT NULL DEFAULT ''::character varying
);

-- Table: public.incomes

DROP TABLE IF EXISTS public.incomes;

CREATE TABLE public.incomes
(
  id SERIAL PRIMARY KEY,
  description character varying NOT NULL DEFAULT ''::character varying,
  accounts_id integer NOT NULL DEFAULT 0,
  deleted integer NOT NULL DEFAULT 0,
  p_id character varying NOT NULL DEFAULT ''::character varying
);

-- Table: public.params

DROP TABLE IF EXISTS public.params;

CREATE TABLE public.params
(
  id SERIAL PRIMARY KEY,
  build integer NOT NULL DEFAULT 0
);


-- Table: public.passwordresets

DROP TABLE IF EXISTS public.passwordresets;

CREATE TABLE public.passwordresets
(
  id SERIAL PRIMARY KEY,
  email character varying NOT NULL DEFAULT ''::character varying,
  token character varying UNIQUE NOT NULL DEFAULT ''::character varying,
  created_at timestamp NOT NULL DEFAULT NOW(),
  done integer NOT NULL DEFAULT 0
  );

-- Table: public.accountsusers

DROP TABLE IF EXISTS public.accountsusers;

CREATE TABLE public.accountsusers
(
  id SERIAL PRIMARY KEY,
  accounts_id integer NOT NULL DEFAULT 0,
  users_id integer NOT NULL DEFAULT 0
);


