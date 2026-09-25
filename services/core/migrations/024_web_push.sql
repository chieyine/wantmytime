-- Phone and browser notifications (self-hosted web push). Email stays the
-- record; these are a nudge for four moments: a new booking and a problem
-- report (sellers), and a call starting in 10 minutes (both people).
CREATE TABLE IF NOT EXISTS push_subscriptions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  endpoint text NOT NULL UNIQUE,
  p256dh text NOT NULL,
  auth text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_success_at timestamptz,
  failures int NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS push_subscriptions_user ON push_subscriptions(user_id);

CREATE TABLE IF NOT EXISTS push_outbox (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  event_key text NOT NULL UNIQUE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  booking_id uuid NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
  kind text NOT NULL CHECK (kind IN ('new_booking_seller', 'problem_reported_seller', 'call_soon_seller', 'call_soon_buyer')),
  state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'sent', 'skipped', 'failed')),
  attempts int NOT NULL DEFAULT 0,
  due_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  sent_at timestamptz
);
CREATE INDEX IF NOT EXISTS push_outbox_due ON push_outbox(due_at) WHERE state = 'queued';
