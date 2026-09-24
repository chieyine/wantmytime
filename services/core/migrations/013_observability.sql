-- Background worker liveness, read by the watchdog and /metrics.
CREATE TABLE IF NOT EXISTS worker_heartbeats (
  name text PRIMARY KEY,
  instance text NOT NULL,
  last_beat_at timestamptz NOT NULL DEFAULT now(),
  last_error_at timestamptz,
  last_error text
);

-- Operational alerts raised by the watchdog. One row per condition; the row
-- remembers when operators were last emailed so reminders are rate limited.
CREATE TABLE IF NOT EXISTS ops_alerts (
  key text PRIMARY KEY,
  state text NOT NULL CHECK (state IN ('firing', 'resolved')),
  severity text NOT NULL CHECK (severity IN ('critical', 'warning')),
  summary text NOT NULL,
  value double precision NOT NULL DEFAULT 0,
  first_fired_at timestamptz NOT NULL DEFAULT now(),
  last_notified_at timestamptz,
  resolved_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ops_alerts_firing ON ops_alerts(updated_at DESC) WHERE state = 'firing';
