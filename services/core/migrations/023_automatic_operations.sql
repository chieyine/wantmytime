-- Operations run themselves: only a genuine disagreement between a buyer
-- and a seller waits for a person.

-- A buyer's problem report goes to the seller first. The seller refunds (in
-- full or in part) or disagrees; silence past the deadline refunds the buyer
-- in full. Only a disagreement reaches operations.
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS issue_respond_by timestamptz;
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS issue_seller_response text;
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS issue_disputed_at timestamptz;
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS issue_seller_note text;
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_issue_seller_response_check;
ALTER TABLE bookings ADD CONSTRAINT bookings_issue_seller_response_check
  CHECK (issue_seller_response IS NULL OR issue_seller_response IN ('refund_full', 'refund_partial', 'disagree', 'no_response'));
CREATE INDEX IF NOT EXISTS bookings_issue_awaiting_seller ON bookings(issue_respond_by)
  WHERE issue_reason IS NOT NULL AND issue_resolved_at IS NULL AND issue_seller_response IS NULL;

-- How many times a failed refund, payment event or email has been retried
-- by itself before anyone is alerted.
ALTER TABLE refunds ADD COLUMN IF NOT EXISTS auto_retries int NOT NULL DEFAULT 0;
ALTER TABLE provider_events ADD COLUMN IF NOT EXISTS auto_retries int NOT NULL DEFAULT 0;
ALTER TABLE notification_outbox ADD COLUMN IF NOT EXISTS auto_retries int NOT NULL DEFAULT 0;

-- Payments that arrive with the wrong amount or currency are returned
-- automatically, like other payments that cannot become a booking.
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
    'meeting_link_auto_seller',
    'problem_disputed_buyer','payout_failed_seller'
  ));
