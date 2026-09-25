-- A seller can publish a fixed price and still take offers; the buyer picks.
ALTER TABLE seller_profiles DROP CONSTRAINT IF EXISTS seller_profiles_mode_check;
ALTER TABLE seller_profiles ADD CONSTRAINT seller_profiles_mode_check CHECK (mode IN ('fixed','offer','both'));
