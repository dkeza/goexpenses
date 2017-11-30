--
-- File generated with SQLiteStudio v3.1.1 on uto apr 11 22:22:56 2017
--
-- Text encoding used: System
--
PRAGMA foreign_keys = off;
BEGIN TRANSACTION;

-- Table: accounts
DROP TABLE IF EXISTS accounts;

CREATE TABLE accounts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT
                        NOT NULL
                        UNIQUE,
    description TEXT    NOT NULL
                        DEFAULT (''),
    deleted     BOOLEAN NOT NULL
                        DEFAULT (0),
    fromdate    TEXT    NOT NULL
                        DEFAULT (''),
    todate      TEXT    NOT NULL
                        DEFAULT ('') 
);


-- Table: accountsusers
DROP TABLE IF EXISTS accountsusers;

CREATE TABLE accountsusers (
    id          INTEGER PRIMARY KEY AUTOINCREMENT
                        UNIQUE
                        DEFAULT (''),
    accounts_id INTEGER REFERENCES accounts (id),
    users_id    INTEGER REFERENCES users (id) 
);


-- Table: currencies
DROP TABLE IF EXISTS currencies;

CREATE TABLE currencies (
    code TEXT            NOT NULL
                         UNIQUE
                         DEFAULT (''),
    id   INTEGER         PRIMARY KEY AUTOINCREMENT
                         UNIQUE,
    rate DECIMAL (12, 4) NOT NULL
                         DEFAULT (0),
    date INTEGER
);


-- Table: expenses
DROP TABLE IF EXISTS expenses;

CREATE TABLE expenses (
    id          INTEGER         NOT NULL
                                PRIMARY KEY AUTOINCREMENT
                                UNIQUE,
    description TEXT            NOT NULL
                                DEFAULT '',
    accounts_id INTEGER         NOT NULL
                                DEFAULT (0),
    amount      DECIMAL (12, 2) NOT NULL
                                DEFAULT (0),
    exchange    DECIMAL (12, 4) NOT NULL
                                DEFAULT (0),
    deleted     BOOLEAN         DEFAULT (0),
    expenses_id INTEGER         NOT NULL
                                DEFAULT (0),
    p_id        TEXT            NOT NULL
                                DEFAULT ('') 
);


-- Table: incomes
DROP TABLE IF EXISTS incomes;

CREATE TABLE incomes (
    id          INTEGER NOT NULL
                        PRIMARY KEY AUTOINCREMENT
                        UNIQUE,
    description TEXT    NOT NULL
                        DEFAULT '',
    accounts_id INTEGER NOT NULL
                        DEFAULT (0),
    deleted     BOOLEAN NOT NULL
                        DEFAULT (0),
    p_id        TEXT    NOT NULL
                        DEFAULT ('') 
);


-- Table: params
DROP TABLE IF EXISTS params;

CREATE TABLE params (
    build INTEGER NOT NULL
                  DEFAULT (0),
    id    INTEGER PRIMARY KEY
                  NOT NULL
);


-- Table: passwordresets
DROP TABLE IF EXISTS passwordresets;

CREATE TABLE passwordresets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT
                       UNIQUE
                       NOT NULL,
    email      TEXT    NOT NULL,
    token      TEXT    UNIQUE
                       NOT NULL,
    created_at INTEGER NOT NULL
                       DEFAULT (CURRENT_TIMESTAMP),
    done       INTEGER DEFAULT (0) 
);


-- Table: posts
DROP TABLE IF EXISTS posts;

CREATE TABLE posts (
    id          INTEGER         NOT NULL
                                PRIMARY KEY AUTOINCREMENT
                                UNIQUE,
    description TEXT            NOT NULL
                                DEFAULT '',
    expenses_id INTEGER         NOT NULL
                                DEFAULT 0,
    incomes_id  INTEGER         NOT NULL
                                DEFAULT 0,
    created_at  INTEGER         NOT NULL
                                DEFAULT CURRENT_TIMESTAMP,
    amount      DECIMAL (12, 2) NOT NULL
                                DEFAULT (0),
    accounts_id INTEGER         NOT NULL
                                DEFAULT (0),
    exchange    DECIMAL (12, 4) DEFAULT (0),
    deleted     BOOLEAN         NOT NULL
                                DEFAULT (0),
    p_id        TEXT            DEFAULT ('') 
);


-- Table: sessions
DROP TABLE IF EXISTS sessions;

CREATE TABLE sessions (
    id                    INTEGER NOT NULL
                                  PRIMARY KEY AUTOINCREMENT
                                  UNIQUE,
    uuid                  TEXT    NOT NULL
                                  DEFAULT ''
                                  UNIQUE,
    user_id               INTEGER NOT NULL
                                  DEFAULT 0,
    created_at            TEXT    NOT NULL
                                  DEFAULT CURRENT_TIMESTAMP,
    lang                  TEXT    NOT NULL
                                  DEFAULT EN,
    message               TEXT    NOT NULL
                                  DEFAULT (''),
    expenses_id           INTEGER NOT NULL
                                  DEFAULT (0),
    last_post_description TEXT    NOT NULL
                                  DEFAULT (''),
    message_success       INTEGER NOT NULL
                                  DEFAULT (0) 
);


-- Table: users
DROP TABLE IF EXISTS users;

CREATE TABLE users (
    id                  INTEGER NOT NULL
                                PRIMARY KEY AUTOINCREMENT
                                UNIQUE,
    name                TEXT    NOT NULL
                                DEFAULT '',
    email               TEXT    NOT NULL
                                DEFAULT ''
                                UNIQUE,
    password            TEXT    NOT NULL
                                DEFAULT '',
    created_at          INTEGER NOT NULL
                                DEFAULT CURRENT_TIMESTAMP,
    username            TEXT    UNIQUE
                                NOT NULL,
    default_accounts_id INTEGER NOT NULL
                                DEFAULT (0),
    lang                TEXT    NOT NULL
                                DEFAULT EN
);


COMMIT TRANSACTION;
PRAGMA foreign_keys = on;
