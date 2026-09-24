ALTER TABLE bookings
  ADD COLUMN calendar_sequence integer NOT NULL DEFAULT 0 CHECK (calendar_sequence >= 0);

CREATE TABLE reschedule_requests (
  id uuid PRIMARY KEY,
  booking_id uuid NOT NULL REFERENCES bookings(id),
  requester_user_id uuid NOT NULL REFERENCES users(id),
  proposed_starts_at timestamptz NOT NULL,
  state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','accepted','declined','expired','withdrawn')),
  version integer NOT NULL DEFAULT 1 CHECK (version > 0),
  expires_at timestamptz NOT NULL,
  responded_by uuid REFERENCES users(id),
  responded_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX reschedule_one_pending_per_booking
  ON reschedule_requests(booking_id) WHERE state='pending';
CREATE INDEX reschedule_participant_history_idx
  ON reschedule_requests(booking_id,created_at DESC);

CREATE TABLE reschedule_events (
  id uuid PRIMARY KEY,
  request_id uuid NOT NULL REFERENCES reschedule_requests(id),
  actor_user_id uuid NOT NULL REFERENCES users(id),
  action text NOT NULL CHECK (action IN ('proposed','accepted','declined','expired','withdrawn')),
  previous_starts_at timestamptz NOT NULL,
  proposed_starts_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION reject_reschedule_event_mutation() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'reschedule_events are append-only';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER reschedule_events_append_only
  BEFORE UPDATE OR DELETE ON reschedule_events
  FOR EACH ROW EXECUTE FUNCTION reject_reschedule_event_mutation();
