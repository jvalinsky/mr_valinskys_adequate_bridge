-- Room database schema for SSB Room Server
-- Based on go-ssb-room v2 schema

-- +migrate Up

CREATE TABLE members (
  id            INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  role          INTEGER NOT NULL,
  pub_key       TEXT    NOT NULL UNIQUE,

  CHECK(role > 0)
);
CREATE INDEX members_pubkeys ON members(pub_key);

CREATE TABLE fallback_passwords (
  id            INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  login         TEXT    NOT NULL UNIQUE,
  password_hash BLOB    NOT NULL,

  member_id     INTEGER NOT NULL,

  FOREIGN KEY ( member_id ) REFERENCES members( "id" )  ON DELETE CASCADE
);
CREATE INDEX fallback_passwords_by_login ON fallback_passwords(login);

CREATE TABLE invites (
  id               INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  hashed_token     TEXT UNIQUE NOT NULL,
  created_by       INTEGER NOT NULL,
  created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  active boolean   NOT NULL DEFAULT TRUE,

  FOREIGN KEY ( created_by ) REFERENCES members( "id" )  ON DELETE CASCADE
);
CREATE INDEX invite_active_ids ON invites(id) WHERE active=TRUE;
CREATE UNIQUE INDEX invite_active_tokens ON invites(hashed_token) WHERE active=TRUE;
CREATE INDEX invite_inactive ON invites(active);

CREATE TABLE aliases (
  id            INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  name          TEXT UNIQUE NOT NULL,
  member_id     INTEGER NOT NULL,
  signature     BLOB NOT NULL,

  FOREIGN KEY ( member_id ) REFERENCES members( "id" )  ON DELETE CASCADE
);
CREATE UNIQUE INDEX aliases_ids ON aliases(id);
CREATE UNIQUE INDEX aliases_names ON aliases(name);

CREATE TABLE denied_keys (
  id          INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  pub_key     TEXT NOT NULL UNIQUE,
  comment     TEXT NOT NULL,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX denied_keys_by_pubkey ON denied_keys(pub_key);

CREATE TABLE room_config (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT INTO room_config (key, value) VALUES ('privacy_mode', 'community');
INSERT INTO room_config (key, value) VALUES ('default_language', 'en');

CREATE TABLE auth_tokens (
  id         INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  token      TEXT UNIQUE NOT NULL,
  member_id  INTEGER NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  FOREIGN KEY ( member_id ) REFERENCES members( "id" )  ON DELETE CASCADE
);
CREATE INDEX auth_tokens_by_token ON auth_tokens(token);
CREATE INDEX auth_tokens_by_member ON auth_tokens(member_id);

-- +migrate Down
DROP TABLE auth_tokens;
DROP TABLE room_config;
DROP TABLE denied_keys;
DROP INDEX denied_keys_by_pubkey;
DROP TABLE aliases;
DROP INDEX aliases_names;
DROP INDEX aliases_ids;
DROP TABLE invites;
DROP INDEX invite_inactive;
DROP INDEX invite_active_tokens;
DROP INDEX invite_active_ids;
DROP TABLE fallback_passwords;
DROP INDEX fallback_passwords_by_login;
DROP TABLE members;
