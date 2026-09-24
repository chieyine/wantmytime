CREATE TABLE notification_outbox (
  id uuid PRIMARY KEY,
  event_key text NOT NULL UNIQUE,
  booking_id uuid NOT NULL REFERENCES bookings(id),
  recipient_user_id uuid NOT NULL REFERENCES users(id),
  kind text NOT NULL CHECK (kind IN (
    'booking_confirmed_buyer','booking_confirmed_seller','meeting_link_ready_buyer',
    'booking_reminder_24h_buyer','booking_reminder_24h_seller',
    'booking_reminder_1h_buyer','booking_reminder_1h_seller',
    'meeting_link_due_24h_seller','meeting_link_due_2h_seller',
    'reschedule_accepted_buyer','reschedule_accepted_seller'
  )),
  due_at timestamptz NOT NULL DEFAULT now(),
  state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','processing','sent','failed','cancelled')),
  attempts smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 8),
  claimed_at timestamptz,
  sent_at timestamptz,
  last_error_code text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notification_outbox_due_idx ON notification_outbox(due_at,id) WHERE state='queued';
CREATE INDEX notification_outbox_stale_claim_idx ON notification_outbox(claimed_at) WHERE state='processing';
