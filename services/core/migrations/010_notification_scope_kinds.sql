ALTER TABLE notification_outbox
  DROP CONSTRAINT IF EXISTS notification_outbox_kind_check;

ALTER TABLE notification_outbox
  ADD CONSTRAINT notification_outbox_kind_check
  CHECK (kind IN (
    'booking_confirmed_buyer','booking_confirmed_seller','meeting_link_ready_buyer',
    'booking_reminder_24h_buyer','booking_reminder_24h_seller',
    'booking_reminder_1h_buyer','booking_reminder_1h_seller',
    'meeting_link_due_24h_seller','meeting_link_due_2h_seller',
    'reschedule_accepted_buyer','reschedule_accepted_seller',
    'cancellation_reviewed_buyer','cancellation_reviewed_seller'
  ));
