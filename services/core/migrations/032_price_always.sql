-- Every link has a price. Taking offers is an extra on top ("both").
UPDATE seller_profiles SET mode='both' WHERE mode='offer';
ALTER TABLE seller_profiles DROP CONSTRAINT IF EXISTS seller_profiles_mode_check;
ALTER TABLE seller_profiles ADD CONSTRAINT seller_profiles_mode_check CHECK (mode IN ('fixed','both'));
