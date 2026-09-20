ALTER TABLE users ADD COLUMN site_filter_mode TEXT NOT NULL DEFAULT '';
UPDATE users SET site_filter_mode='deny' WHERE site_deny_categories!='' OR site_deny_domains!='';
