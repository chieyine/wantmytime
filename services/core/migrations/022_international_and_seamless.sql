-- WantMyTime starts in Nigeria but is not Nigeria-only. Each seller now has a
-- country and is priced, charged and paid in that country's currency. Money
-- columns stay integer minor units; only the NGN-only checks go.
ALTER TABLE seller_profiles ADD COLUMN IF NOT EXISTS country char(2) NOT NULL DEFAULT 'NG';
ALTER TABLE seller_profiles ADD COLUMN IF NOT EXISTS currency char(3) NOT NULL DEFAULT 'NGN';
ALTER TABLE seller_profiles DROP CONSTRAINT IF EXISTS seller_profiles_country_shape;
ALTER TABLE seller_profiles ADD CONSTRAINT seller_profiles_country_shape CHECK (country ~ '^[A-Z]{2}$' AND currency ~ '^[A-Z]{3}$');

-- The original NGN-only checks were unnamed column constraints, so find them
-- by definition rather than by generated name.
DO $$
DECLARE c record;
BEGIN
  FOR c IN SELECT conrelid::regclass AS tbl, conname FROM pg_constraint
           WHERE contype = 'c' AND conrelid IN ('pricing_versions'::regclass, 'quotes'::regclass)
             AND pg_get_constraintdef(oid) LIKE '%NGN%'
  LOOP
    EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I', c.tbl, c.conname);
  END LOOP;
END $$;
ALTER TABLE pricing_versions DROP CONSTRAINT IF EXISTS pricing_versions_currency_shape;
ALTER TABLE pricing_versions ADD CONSTRAINT pricing_versions_currency_shape CHECK (currency ~ '^[A-Z]{3}$');
ALTER TABLE quotes DROP CONSTRAINT IF EXISTS quotes_currency_shape;
ALTER TABLE quotes ADD CONSTRAINT quotes_currency_shape CHECK (currency ~ '^[A-Z]{3}$');

-- Payouts to a bank account or, where people are paid that way (Ghana,
-- Kenya), a mobile money wallet. For mobile money, bank_code holds the
-- network operator and the sealed number is the wallet's phone number.
ALTER TABLE seller_payout_accounts ADD COLUMN IF NOT EXISTS destination_type text NOT NULL DEFAULT 'bank_account';
ALTER TABLE seller_payout_accounts DROP CONSTRAINT IF EXISTS seller_payout_accounts_destination_type_check;
ALTER TABLE seller_payout_accounts ADD CONSTRAINT seller_payout_accounts_destination_type_check CHECK (destination_type IN ('bank_account', 'mobile_money'));
ALTER TABLE seller_payouts ADD COLUMN IF NOT EXISTS destination_type text NOT NULL DEFAULT 'bank_account';
ALTER TABLE seller_payouts DROP CONSTRAINT IF EXISTS seller_payouts_destination_type_check;
ALTER TABLE seller_payouts ADD CONSTRAINT seller_payouts_destination_type_check CHECK (destination_type IN ('bank_account', 'mobile_money'));

-- A payment that could not become a booking (it arrived after the time was
-- taken, or the buyer paid twice) is refunded automatically. Such a refund
-- belongs to the payment exception, not to a booking.
ALTER TABLE refunds ALTER COLUMN booking_id DROP NOT NULL;
ALTER TABLE refunds ADD COLUMN IF NOT EXISTS payment_exception_id uuid REFERENCES payment_exceptions(id);
CREATE UNIQUE INDEX IF NOT EXISTS refunds_one_per_exception ON refunds(payment_exception_id) WHERE payment_exception_id IS NOT NULL AND state <> 'failed';
ALTER TABLE refunds DROP CONSTRAINT IF EXISTS refunds_subject_check;
ALTER TABLE refunds ADD CONSTRAINT refunds_subject_check CHECK (booking_id IS NOT NULL OR payment_exception_id IS NOT NULL);
ALTER TABLE refunds DROP CONSTRAINT IF EXISTS refunds_reason_check;
ALTER TABLE refunds ADD CONSTRAINT refunds_reason_check CHECK (reason IN ('buyer_cancelled', 'seller_cancelled', 'seller_no_show', 'operator', 'problem_upheld', 'unbooked_payment'));

-- Emails about such a payment point at the exception.
ALTER TABLE notification_outbox ADD COLUMN IF NOT EXISTS payment_exception_id uuid REFERENCES payment_exceptions(id);
ALTER TABLE notification_outbox DROP CONSTRAINT IF EXISTS notification_outbox_subject_check;
ALTER TABLE notification_outbox ADD CONSTRAINT notification_outbox_subject_check
  CHECK (num_nonnulls(booking_id, offer_id, payment_exception_id) = 1);

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
    'review_request_buyer','review_received_seller',
    'payout_sent_seller','problem_reported_seller',
    'problem_resolved_buyer','problem_resolved_seller',
    'payment_returning_buyer','payment_returned_buyer',
    'meeting_link_auto_seller'
  ));

-- A meeting link WantMyTime created because the seller had not added one.
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_meeting_source_check;
ALTER TABLE bookings ADD CONSTRAINT bookings_meeting_source_check CHECK (meeting_source IS NULL OR meeting_source IN ('seller', 'google_meet', 'auto'));

-- A buyer's request to cancel outside the policy goes to the seller, who can
-- cancel (a full refund) or let the booking stand. It no longer holds the
-- payout; open requests close by themselves when the booking time arrives.
CREATE INDEX IF NOT EXISTS booking_cancellation_open ON booking_cancellation_requests(booking_id) WHERE state = 'open';
