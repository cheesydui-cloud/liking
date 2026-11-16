CREATE TABLE IF NOT EXISTS traffic_forwards (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  entry_server_id INTEGER NOT NULL,
  entry_port INTEGER NOT NULL,
  landing_inbound_id INTEGER NOT NULL,
  landing_dial_host TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS traffic_forward_hops (
  forward_id INTEGER NOT NULL,
  position INTEGER NOT NULL,
  server_id INTEGER NOT NULL,
  listen_port INTEGER NOT NULL,
  dial_host TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (forward_id, position)
);

CREATE TABLE IF NOT EXISTS package_forwards (
  package_id INTEGER NOT NULL,
  forward_id INTEGER NOT NULL,
  PRIMARY KEY (package_id, forward_id)
);
