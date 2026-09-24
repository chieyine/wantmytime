-- Email is the only notification channel, so every event that needs the other
-- person to act (or to know) gets an email. Offers are not bookings yet, so an
-- outbox row now points at exactly one of a booking or an offer. reference_id
-- names the specific reschedule request or cancellation request, so the email
-- shows the right proposed time even after later requests.
ALTER TABLE notification_outbox ALTER COLUMN booking_id DROP NOT NULL;
ALTER TABLE notification_outbox ADD COLUMN IF NOT EXISTS offer_id uuid REFERENCES offers(id);
ALTER TABLE notification_outbox ADD COLUMN IF NOT EXISTS reference_id uuid;
ALTER TABLE notification_outbox DROP CONSTRAINT IF EXISTS notification_outbox_subject_check;
ALTER TABLE notification_outbox ADD CONSTRAINT notification_outbox_subject_check
  CHECK ((booking_id IS NOT NULL) <> (offer_id IS NOT NULL));
CREATE INDEX IF NOT EXISTS notification_outbox_offer_idx ON notification_outbox(offer_id) WHERE offer_id IS NOT NULL;

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
    'offer_withdrawn_buyer','offer_withdrawn_seller'
  ));
