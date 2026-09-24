-- Optional Google Calendar connection per seller. The refresh token is
-- encrypted with CALENDAR_TOKEN_ENCRYPTION_KEY; access tokens are never stored.
CREATE TABLE IF NOT EXISTS calendar_connections (
  seller_id uuid PRIMARY KEY REFERENCES seller_profiles(id),
  provider text NOT NULL DEFAULT 'google' CHECK (provider = 'google'),
  account_email text NOT NULL,
  encrypted_refresh_token bytea NOT NULL,
  scopes text NOT NULL,
  check_busy boolean NOT NULL DEFAULT true,
  add_events boolean NOT NULL DEFAULT true,
  create_meet_links boolean NOT NULL DEFAULT true,
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'error', 'revoked')),
  last_error text,
  last_synced_at timestamptz,
  connected_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- Busy time copied from the seller's primary Google calendar. Bookable slots
-- avoid these ranges. Rows are replaced wholesale on each sync.
CREATE TABLE IF NOT EXISTS calendar_busy_blocks (
  seller_id uuid NOT NULL REFERENCES seller_profiles(id),
  busy tstzrange NOT NULL CHECK (NOT isempty(busy)),
  synced_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS calendar_busy_blocks_lookup ON calendar_busy_blocks USING gist (seller_id, busy);

-- Short-lived OAuth state for the connect flow (PKCE verifier kept server-side).
CREATE TABLE IF NOT EXISTS oauth_states (
  state_hash bytea PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id),
  code_verifier text NOT NULL,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- The Google event that mirrors a booking, and where its meeting link came from.
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS calendar_event_id text;
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS meeting_source text CHECK (meeting_source IS NULL OR meeting_source IN ('seller', 'google_meet'));

-- At most one queued sync per booking: the worker reads the booking's current state
-- and makes the calendar match (create, move or remove the event).
CREATE TABLE IF NOT EXISTS calendar_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  booking_id uuid NOT NULL REFERENCES bookings(id),
  state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'processing', 'done', 'failed')),
  attempts smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 10),
  due_at timestamptz NOT NULL DEFAULT now(),
  claimed_at timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS calendar_jobs_one_queued ON calendar_jobs(booking_id) WHERE state = 'queued';
CREATE INDEX IF NOT EXISTS calendar_jobs_due ON calendar_jobs(due_at) WHERE state = 'queued';
