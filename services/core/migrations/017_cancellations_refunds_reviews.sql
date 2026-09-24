-- Phase 5: cancellation policies, self-serve cancellation, refunds with seller
-- recovery, no-show reports and verified reviews.

-- Cancellation policy chosen by the seller, and the copy frozen onto each
-- booking so a later change never alters an existing booking's terms.
ALTER TABLE seller_profiles ADD COLUMN IF NOT EXISTS cancellation_policy text NOT NULL DEFAULT 'flexible';
ALTER TABLE seller_profiles DROP CONSTRAINT IF EXISTS seller_profiles_cancellation_policy_check;
ALTER TABLE seller_profiles ADD CONSTRAINT seller_profiles_cancellation_policy_check CHECK (cancellation_policy IN ('flexible', 'moderate', 'strict'));

ALTER TABLE bookings ADD COLUMN IF NOT EXISTS cancellation_policy text;
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS cancelled_at timestamptz;
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS cancelled_by_role text CHECK (cancelled_by_role IS NULL OR cancelled_by_role IN ('buyer', 'seller', 'operator'));
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS cancellation_reason text;
UPDATE bookings SET cancellation_policy = 'flexible' WHERE cancellation_policy IS NULL;
ALTER TABLE bookings ALTER COLUMN cancellation_policy SET NOT NULL;
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_cancellation_policy_check;
ALTER TABLE bookings ADD CONSTRAINT bookings_cancellation_policy_check CHECK (cancellation_policy IN ('flexible', 'moderate', 'strict'));
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_state_check;
ALTER TABLE bookings ADD CONSTRAINT bookings_state_check CHECK (state IN ('confirmed', 'completed', 'cancelled', 'no_show_buyer', 'no_show_seller'));

-- Every booking insert path gets the seller's current policy unless one is given.
CREATE OR REPLACE FUNCTION snapshot_cancellation_policy() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.cancellation_policy IS NULL THEN
    SELECT cancellation_policy INTO NEW.cancellation_policy FROM seller_profiles WHERE id = NEW.seller_id;
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS bookings_snapshot_policy ON bookings;
CREATE TRIGGER bookings_snapshot_policy BEFORE INSERT ON bookings FOR EACH ROW EXECUTE FUNCTION snapshot_cancellation_policy();

-- Refunds to buyers. Paystack debits refunds from the platform balance even
-- though the seller's share was split to them, so the seller share becomes a
-- recovery recorded in seller_recoveries once the refund is processed.
CREATE TABLE IF NOT EXISTS refunds (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  booking_id uuid NOT NULL REFERENCES bookings(id),
  payment_attempt_id uuid REFERENCES payment_attempts(id),
  amount_minor bigint NOT NULL CHECK (amount_minor > 0),
  currency char(3) NOT NULL,
  platform_share_minor bigint NOT NULL CHECK (platform_share_minor >= 0),
  seller_share_minor bigint NOT NULL CHECK (seller_share_minor >= 0),
  reason text NOT NULL CHECK (reason IN ('buyer_cancelled', 'seller_cancelled', 'seller_no_show', 'operator')),
  state text NOT NULL CHECK (state IN ('pending_approval', 'queued', 'submitted', 'processed', 'failed', 'not_required')),
  provider_refund_id text,
  attempts smallint NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  last_error text,
  requested_by uuid REFERENCES users(id),
  approved_by uuid REFERENCES users(id),
  note text,
  created_at timestamptz NOT NULL DEFAULT now(),
  submitted_at timestamptz,
  processed_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (platform_share_minor + seller_share_minor = amount_minor)
);
-- One refund per booking keeps the arithmetic simple and prevents double refunds.
CREATE UNIQUE INDEX IF NOT EXISTS refunds_one_per_booking ON refunds(booking_id) WHERE state <> 'failed';
CREATE INDEX IF NOT EXISTS refunds_work ON refunds(next_attempt_at) WHERE state IN ('queued', 'submitted');
CREATE UNIQUE INDEX IF NOT EXISTS refunds_provider_id ON refunds(provider_refund_id) WHERE provider_refund_id IS NOT NULL;

-- Money a seller owes back after a refund, recovered from later bookings by
-- taking part of their split share.
CREATE TABLE IF NOT EXISTS seller_recoveries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  seller_id uuid NOT NULL REFERENCES seller_profiles(id),
  refund_id uuid NOT NULL UNIQUE REFERENCES refunds(id),
  amount_minor bigint NOT NULL CHECK (amount_minor > 0),
  recovered_minor bigint NOT NULL DEFAULT 0 CHECK (recovered_minor >= 0 AND recovered_minor <= amount_minor),
  currency char(3) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS seller_recoveries_open ON seller_recoveries(seller_id, created_at) WHERE recovered_minor < amount_minor;

-- A checkout's recovery portion is frozen with its fee snapshot.
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS recovery_minor bigint NOT NULL DEFAULT 0 CHECK (recovery_minor >= 0);
ALTER TABLE payment_allocations ADD COLUMN IF NOT EXISTS recovery_minor bigint NOT NULL DEFAULT 0 CHECK (recovery_minor >= 0);
-- The original split identity (gross = deduction + entitlement) was an unnamed
-- constraint, so find it by definition rather than by generated name.
DO $$
DECLARE c record;
BEGIN
  FOR c IN SELECT conname FROM pg_constraint
           WHERE conrelid = 'payment_allocations'::regclass AND contype = 'c'
             AND pg_get_constraintdef(oid) = 'CHECK ((gross_minor = (deduction_minor + seller_entitlement_minor)))'
  LOOP
    EXECUTE format('ALTER TABLE payment_allocations DROP CONSTRAINT %I', c.conname);
  END LOOP;
END $$;
ALTER TABLE payment_allocations DROP CONSTRAINT IF EXISTS payment_allocations_split_identity;
ALTER TABLE payment_allocations ADD CONSTRAINT payment_allocations_split_identity CHECK (gross_minor = deduction_minor + seller_entitlement_minor + recovery_minor);

-- No-show reports. The absent person has until resolves_at to dispute; after
-- that the report stands.
CREATE TABLE IF NOT EXISTS no_show_reports (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  booking_id uuid NOT NULL UNIQUE REFERENCES bookings(id),
  reporter_user_id uuid NOT NULL REFERENCES users(id),
  absent_role text NOT NULL CHECK (absent_role IN ('buyer', 'seller')),
  state text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'disputed', 'accepted', 'rejected')),
  dispute_reason text,
  resolves_at timestamptz NOT NULL,
  resolution text,
  resolved_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz
);
CREATE INDEX IF NOT EXISTS no_show_reports_due ON no_show_reports(resolves_at) WHERE state = 'open';

-- Verified reviews: one per booking, by its buyer, after the session.
CREATE TABLE IF NOT EXISTS reviews (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  booking_id uuid NOT NULL UNIQUE REFERENCES bookings(id),
  seller_id uuid NOT NULL REFERENCES seller_profiles(id),
  buyer_user_id uuid NOT NULL REFERENCES users(id),
  reviewer_name text NOT NULL,
  rating smallint NOT NULL CHECK (rating BETWEEN 1 AND 5),
  body text NOT NULL DEFAULT '' CHECK (length(body) <= 1000),
  seller_reply text CHECK (seller_reply IS NULL OR length(seller_reply) <= 1000),
  replied_at timestamptz,
  hidden_at timestamptz,
  hidden_reason text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS reviews_public ON reviews(seller_id, created_at DESC) WHERE hidden_at IS NULL;

-- New email kinds, and reference_id for refunds, reports and reviews.
ALTER TABLE notification_outbox DROP CONSTRAINT IF EXISTS notification_outbox_kind_check;
ALTER TABLE notification_outbox ADD CONSTRAINT notification_outbox_kind_check
  CHECK (kind IN (
    'booking_confirmed_buyer','booking_confirmed_seller','meeting_link_ready_buyer',
    'booking_reminder_24h_buyer','booking_reminder_24h_seller',
    'booking_reminder_1h_buyer','booking_reminder_1h_seller',
    'meeting_link_due_24h_seller','meeting_link_due_2h_seller',
    'reschedule_accepted_buyer','reschedule_accepted_seller',
    'reschedule_requested_buyer','reschedule_requested_seller',
    'reschedule_declined_buyer','reschedule_declined_seller',
    'cancellation_requested_buyer','cancellation_requested_seller',
    'cancellation_reviewed_buyer','cancellation_reviewed_seller',
    'offer_received_seller','offer_countered_buyer',
    'offer_accepted_buyer','offer_accepted_seller',
    'offer_declined_buyer','offer_declined_seller',
    'offer_withdrawn_buyer','offer_withdrawn_seller',
    'booking_cancelled_buyer','booking_cancelled_seller',
    'refund_processed_buyer',
    'no_show_reported_buyer','no_show_reported_seller',
    'no_show_resolved_buyer','no_show_resolved_seller',
    'review_request_buyer','review_received_seller'
  ));
