ALTER TABLE users ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
ALTER TABLE packages ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
ALTER TABLE servers ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
ALTER TABLE inbounds ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;

UPDATE users SET sort_order = id WHERE sort_order = 0;
UPDATE packages SET sort_order = id WHERE sort_order = 0;
UPDATE servers SET sort_order = id WHERE sort_order = 0;
UPDATE inbounds SET sort_order = id WHERE sort_order = 0;

CREATE TRIGGER users_sort_order AFTER INSERT ON users
WHEN NEW.sort_order = 0
BEGIN
  UPDATE users SET sort_order = NEW.id WHERE id = NEW.id;
END;

CREATE TRIGGER packages_sort_order AFTER INSERT ON packages
WHEN NEW.sort_order = 0
BEGIN
  UPDATE packages SET sort_order = NEW.id WHERE id = NEW.id;
END;

CREATE TRIGGER servers_sort_order AFTER INSERT ON servers
WHEN NEW.sort_order = 0
BEGIN
  UPDATE servers SET sort_order = NEW.id WHERE id = NEW.id;
END;

CREATE TRIGGER inbounds_sort_order AFTER INSERT ON inbounds
WHEN NEW.sort_order = 0
BEGIN
  UPDATE inbounds SET sort_order = NEW.id WHERE id = NEW.id;
END;
