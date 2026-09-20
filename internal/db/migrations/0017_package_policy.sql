ALTER TABLE packages ADD COLUMN speed_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE packages ADD COLUMN sub_rule_preset TEXT NOT NULL DEFAULT '';
ALTER TABLE packages ADD COLUMN sub_rule_categories TEXT NOT NULL DEFAULT '[]';
ALTER TABLE packages ADD COLUMN site_deny_categories TEXT NOT NULL DEFAULT '[]';
ALTER TABLE packages ADD COLUMN site_deny_domains TEXT NOT NULL DEFAULT '[]';
ALTER TABLE packages ADD COLUMN site_filter_mode TEXT NOT NULL DEFAULT '';
