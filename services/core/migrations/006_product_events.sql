ALTER TABLE product_events ADD COLUMN IF NOT EXISTS seller_id uuid REFERENCES seller_profiles(id);
ALTER TABLE product_events ADD COLUMN IF NOT EXISTS event_day date NOT NULL DEFAULT (now() AT TIME ZONE 'UTC')::date;
ALTER TABLE product_events ALTER COLUMN environment SET DEFAULT 'production';
ALTER TABLE product_events ALTER COLUMN occurred_at SET DEFAULT now();
CREATE INDEX IF NOT EXISTS product_events_day_name ON product_events(event_day,event_name);

CREATE TABLE IF NOT EXISTS booking_cancellation_requests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  booking_id uuid NOT NULL REFERENCES bookings(id),
  requester_user_id uuid NOT NULL REFERENCES users(id),
  reason text NOT NULL CHECK (length(reason) BETWEEN 8 AND 500),
  state text NOT NULL DEFAULT 'open' CHECK (state IN ('open','resolved')),
  resolution text,
  created_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS booking_cancellation_one_open ON booking_cancellation_requests(booking_id) WHERE state='open';
