CREATE TABLE package_servers (
  package_id INTEGER NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
  server_id  INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
  multiplier REAL    NOT NULL DEFAULT 1,
  PRIMARY KEY (package_id, server_id)
);
CREATE INDEX idx_package_servers_server ON package_servers(server_id);
