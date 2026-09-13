-- Login password kept for admin 名片 copy. Hash remains the source of truth for auth.
ALTER TABLE users ADD COLUMN password_plain TEXT NOT NULL DEFAULT '';
