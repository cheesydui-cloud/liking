CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  username      TEXT    NOT NULL UNIQUE,
  password_hash TEXT    NOT NULL,
  role          TEXT    NOT NULL CHECK(role IN ('admin','user')),
  remark        TEXT    NOT NULL DEFAULT '',
  enabled       INTEGER NOT NULL DEFAULT 1,
  expires_at    INTEGER NOT NULL DEFAULT 0,
  traffic_limit INTEGER, -- bytes; NULL = follow package; 0 = unlimited
  used_up       INTEGER NOT NULL DEFAULT 0,
  used_down     INTEGER NOT NULL DEFAULT 0,
  cycle_start   INTEGER NOT NULL DEFAULT 0,
  sub_token     TEXT    NOT NULL UNIQUE,
  created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
  token      TEXT    PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_exp  ON sessions(expires_at);

CREATE TABLE servers (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT    NOT NULL,
  public_host  TEXT    NOT NULL DEFAULT '',
  token        TEXT    NOT NULL UNIQUE,
  online       INTEGER NOT NULL DEFAULT 0,
  last_seen    INTEGER NOT NULL DEFAULT 0,
  agent_ver    TEXT    NOT NULL DEFAULT '',
  os           TEXT    NOT NULL DEFAULT '',
  arch         TEXT    NOT NULL DEFAULT '',
  connect_ip   TEXT    NOT NULL DEFAULT '',
  config_rev   TEXT    NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL
);

CREATE TABLE certificates (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  name     TEXT    NOT NULL,
  cert_pem TEXT    NOT NULL,
  key_pem  TEXT    NOT NULL,
  domains  TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE packages (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT    NOT NULL,
  traffic_bytes INTEGER NOT NULL DEFAULT 0,
  cycle_days    INTEGER NOT NULL DEFAULT 30,
  reset_day     INTEGER NOT NULL DEFAULT 0,
  direction     TEXT    NOT NULL DEFAULT 'oneway' CHECK(direction IN ('oneway','twoway')),
  created_at    INTEGER NOT NULL
);

CREATE TABLE inbounds (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id       INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
  name            TEXT    NOT NULL,
  profile         TEXT    NOT NULL,
  protocol        TEXT    NOT NULL,
  network         TEXT    NOT NULL,
  security        TEXT    NOT NULL,
  core            TEXT    NOT NULL,
  listen          TEXT    NOT NULL DEFAULT '0.0.0.0',
  port            INTEGER NOT NULL,
  enabled         INTEGER NOT NULL DEFAULT 1,
  settings        TEXT    NOT NULL DEFAULT '{}',
  cert_id         INTEGER REFERENCES certificates(id) ON DELETE SET NULL,
  line_kind       TEXT    NOT NULL DEFAULT 'direct' CHECK(line_kind IN ('direct','chain')),
  exit_inbound_id INTEGER REFERENCES inbounds(id) ON DELETE SET NULL,
  exit_uri        TEXT    NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL
);
CREATE INDEX idx_inbounds_server ON inbounds(server_id);

CREATE TABLE clients (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  inbound_id INTEGER NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email      TEXT    NOT NULL,
  uuid       TEXT    NOT NULL DEFAULT '',
  password   TEXT    NOT NULL DEFAULT '',
  username   TEXT    NOT NULL DEFAULT '',
  enabled    INTEGER NOT NULL DEFAULT 1,
  UNIQUE(inbound_id, user_id)
);
CREATE INDEX idx_clients_user ON clients(user_id);

CREATE TABLE package_inbounds (
  package_id INTEGER NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
  inbound_id INTEGER NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
  multiplier REAL    NOT NULL DEFAULT 1,
  PRIMARY KEY (package_id, inbound_id)
);

CREATE TABLE user_packages (
  user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  package_id INTEGER NOT NULL REFERENCES packages(id),
  bound_at   INTEGER NOT NULL,
  expires_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE traffic_daily (
  day        TEXT    NOT NULL,
  user_id    INTEGER NOT NULL,
  inbound_id INTEGER NOT NULL,
  up         INTEGER NOT NULL DEFAULT 0,
  down       INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (day, user_id, inbound_id)
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE audit (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  at      INTEGER NOT NULL,
  user_id INTEGER,
  action  TEXT    NOT NULL,
  detail  TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX idx_audit_at ON audit(at);
