-- One cancellation rule for every booking (full refund if cancelled at least
-- 24 hours before the start), so nothing per seller or per booking is stored.
DROP TRIGGER IF EXISTS bookings_snapshot_policy ON bookings;
DROP FUNCTION IF EXISTS snapshot_cancellation_policy();
ALTER TABLE bookings DROP COLUMN IF EXISTS cancellation_policy;
ALTER TABLE seller_profiles DROP COLUMN IF EXISTS cancellation_policy;
