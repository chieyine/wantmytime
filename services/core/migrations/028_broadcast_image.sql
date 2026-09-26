-- An optional picture across the top of an announcement (an https address).
ALTER TABLE broadcasts ADD COLUMN IF NOT EXISTS image_url text;
