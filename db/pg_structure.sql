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


-- Table: public.sessions

DROP TABLE IF EXISTS public.sessions;

CREATE TABLE public.sessions
(
  id SERIAL PRIMARY KEY,
  uuid character varying unique NOT NULL DEFAULT ''::character varying,
  user_id integer NOT NULL DEFAULT 0,
  lang character varying NOT NULL DEFAULT 'EN'::character varying,
  message character varying NOT NULL DEFAULT ''::character varying,
  expenses_id integer NOT NULL DEFAULT 0,
  last_post_description character varying NOT NULL DEFAULT ''::character varying,
  message_success integer NOT NULL DEFAULT 0
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
  deleted boolean NOT NULL DEFAULT false,
  p_id character varying NOT NULL DEFAULT ''::character varying
  );
  
  