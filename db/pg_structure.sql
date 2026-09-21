-- Table: public.accounts

CREATE TABLE public.accounts
(
  id SERIAL PRIMARY KEY,
  description character varying NOT NULL DEFAULT ''::character varying,
  deleted integer NOT NULL DEFAULT 0,
  fromdate character varying NOT NULL DEFAULT ''::character varying,
  todate character varying NOT NULL DEFAULT ''::character varying
);

-- Table: public.users

CREATE TABLE public.users
(
  id SERIAL PRIMARY KEY,
  name character varying NOT NULL DEFAULT ''::character varying,
  email character varying NOT NULL DEFAULT ''::character varying,
  password character varying NOT NULL DEFAULT ''::character varying,
  created_at timestamp NOT NULL DEFAULT NOW(),
  username character varying NOT NULL DEFAULT ''::character varying,
  default_accounts_id integer NOT NULL DEFAULT 0,
  lang character varying NOT NULL DEFAULT 'EN'::character varying,
  CONSTRAINT users_name_not_blank CHECK (btrim(name) <> ''),
  CONSTRAINT users_username_not_blank CHECK (btrim(username) <> ''),
  CONSTRAINT users_email_not_blank CHECK (btrim(email) <> ''),
  CONSTRAINT users_default_account_fk FOREIGN KEY (default_accounts_id) REFERENCES public.accounts(id)
);


-- Table: public.sessions

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
  p_id character varying NOT NULL DEFAULT ''::character varying,
  created_ts timestamp NOT NULL DEFAULT NOW(),
  CONSTRAINT posts_public_id_not_blank CHECK (btrim(p_id) <> ''),
  CONSTRAINT posts_account_fk FOREIGN KEY (accounts_id) REFERENCES public.accounts(id)
  );

-- Table: public.currencies

CREATE TABLE public.currencies
(
  id SERIAL PRIMARY KEY,
  code character varying UNIQUE NOT NULL DEFAULT ''::character varying,
  rate decimal(12,4) NOT NULL DEFAULT 0,
  date date
);

-- Table: public.expenses

CREATE TABLE public.expenses
(
  id SERIAL PRIMARY KEY,
  description character varying NOT NULL DEFAULT ''::character varying,
  accounts_id integer NOT NULL DEFAULT 0,
  amount decimal(12,2) NOT NULL DEFAULT 0,
  exchange decimal(12,4) NOT NULL DEFAULT 0,
  deleted integer NOT NULL DEFAULT 0,
  expenses_id integer NOT NULL DEFAULT 0,
  p_id character varying NOT NULL DEFAULT ''::character varying,
  CONSTRAINT expenses_public_id_not_blank CHECK (btrim(p_id) <> ''),
  CONSTRAINT expenses_account_fk FOREIGN KEY (accounts_id) REFERENCES public.accounts(id)
);

-- Table: public.incomes

CREATE TABLE public.incomes
(
  id SERIAL PRIMARY KEY,
  description character varying NOT NULL DEFAULT ''::character varying,
  accounts_id integer NOT NULL DEFAULT 0,
  deleted integer NOT NULL DEFAULT 0,
  p_id character varying NOT NULL DEFAULT ''::character varying,
  CONSTRAINT incomes_public_id_not_blank CHECK (btrim(p_id) <> ''),
  CONSTRAINT incomes_account_fk FOREIGN KEY (accounts_id) REFERENCES public.accounts(id)
);

-- Table: public.params

CREATE TABLE public.params
(
  id SERIAL PRIMARY KEY,
  build integer NOT NULL DEFAULT 0
);


-- Table: public.passwordresets

CREATE TABLE public.passwordresets
(
  id SERIAL PRIMARY KEY,
  email character varying NOT NULL DEFAULT ''::character varying,
  token character varying UNIQUE NOT NULL DEFAULT ''::character varying,
  created_at timestamp NOT NULL DEFAULT NOW(),
  done integer NOT NULL DEFAULT 0
  );

-- Table: public.accountsusers

CREATE TABLE public.accountsusers
(
  id SERIAL PRIMARY KEY,
  accounts_id integer NOT NULL DEFAULT 0,
  users_id integer NOT NULL DEFAULT 0,
  CONSTRAINT accountsusers_account_fk FOREIGN KEY (accounts_id) REFERENCES public.accounts(id),
  CONSTRAINT accountsusers_user_fk FOREIGN KEY (users_id) REFERENCES public.users(id)
);

CREATE UNIQUE INDEX users_username_lower_uidx
  ON public.users (lower(btrim(username)))
  WHERE btrim(username) <> '';
CREATE UNIQUE INDEX users_email_lower_uidx
  ON public.users (lower(btrim(email)))
  WHERE btrim(email) <> '';
CREATE UNIQUE INDEX posts_public_id_uidx ON public.posts (p_id) WHERE p_id <> '';
CREATE UNIQUE INDEX expenses_public_id_uidx ON public.expenses (p_id) WHERE p_id <> '';
CREATE UNIQUE INDEX incomes_public_id_uidx ON public.incomes (p_id) WHERE p_id <> '';
CREATE UNIQUE INDEX accountsusers_account_user_uidx ON public.accountsusers (accounts_id, users_id);

CREATE INDEX posts_active_account_date_id_idx
  ON public.posts (accounts_id, created_at DESC, id DESC)
  WHERE deleted = 0;
CREATE INDEX expenses_active_account_description_idx
  ON public.expenses (accounts_id, description)
  WHERE deleted = 0;
CREATE INDEX incomes_active_account_description_idx
  ON public.incomes (accounts_id, description)
  WHERE deleted = 0;
CREATE INDEX sessions_created_at_idx ON public.sessions (created_at);
CREATE INDEX passwordresets_active_email_created_idx
  ON public.passwordresets (email, created_at DESC)
  WHERE done = 0;
CREATE INDEX accountsusers_user_idx ON public.accountsusers (users_id);
