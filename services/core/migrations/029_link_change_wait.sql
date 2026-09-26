-- A link can change once every six months. The seller gets an email when they
-- can change it again.
ALTER TABLE seller_profiles ADD COLUMN IF NOT EXISTS handle_changed_at timestamptz;
ALTER TABLE seller_profiles ADD COLUMN IF NOT EXISTS handle_reminder_due timestamptz;
CREATE INDEX IF NOT EXISTS seller_profiles_handle_reminder_due ON seller_profiles(handle_reminder_due) WHERE handle_reminder_due IS NOT NULL;
