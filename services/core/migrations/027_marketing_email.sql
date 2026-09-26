-- Announcement email (news from WantMyTime and Kredit Technologies).
-- Kept apart from transactional email: booking and payment emails never
-- depend on any of this.

-- One row per address. Only rows that are subscribed AND confirmed are ever
-- emailed or exported. An address is confirmed when its owner proved it with a
-- sign-in code, or when a booking paid for with it went through.
CREATE TABLE IF NOT EXISTS marketing_contacts (
  email text PRIMARY KEY,
  user_id uuid REFERENCES users(id) ON DELETE SET NULL,
  subscribed boolean NOT NULL,
  source text NOT NULL,
  wording text NOT NULL,
  confirmed_at timestamptz,
  unsubscribe_token text NOT NULL UNIQUE,
  consented_at timestamptz,
  unsubscribed_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT marketing_contacts_source CHECK (source IN ('seller_signup','booking','offer','settings','unsubscribe_link','ops_import'))
);
CREATE INDEX IF NOT EXISTS marketing_contacts_audience ON marketing_contacts(email) WHERE subscribed AND confirmed_at IS NOT NULL;

-- Proof of consent: every change, with the exact wording shown. Append-only.
CREATE TABLE IF NOT EXISTS marketing_consent_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email text NOT NULL,
  subscribed boolean NOT NULL,
  source text NOT NULL,
  wording text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS marketing_consent_events_email ON marketing_consent_events(email, created_at);
CREATE OR REPLACE FUNCTION marketing_consent_events_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  -- Erasing a person (account deletion) may remove their rows; nothing may rewrite them.
  IF TG_OP = 'UPDATE' THEN RAISE EXCEPTION 'marketing_consent_events is append-only'; END IF;
  RETURN OLD;
END $$;
DROP TRIGGER IF EXISTS marketing_consent_events_no_update ON marketing_consent_events;
CREATE TRIGGER marketing_consent_events_no_update BEFORE UPDATE ON marketing_consent_events
  FOR EACH ROW EXECUTE FUNCTION marketing_consent_events_append_only();

-- Announcements written and sent from the ops panel.
CREATE TABLE IF NOT EXISTS broadcasts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 150),
  heading text NOT NULL CHECK (length(heading) BETWEEN 1 AND 150),
  body text NOT NULL CHECK (length(body) BETWEEN 1 AND 20000),
  action_label text,
  action_url text,
  state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft','sending','sent','cancelled')),
  created_by uuid NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  queued_at timestamptz,
  finished_at timestamptz,
  CONSTRAINT broadcasts_action_pair CHECK ((action_label IS NULL) = (action_url IS NULL))
);

CREATE TABLE IF NOT EXISTS broadcast_deliveries (
  broadcast_id uuid NOT NULL REFERENCES broadcasts(id) ON DELETE CASCADE,
  email text NOT NULL,
  state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','sent','failed','skipped')),
  attempts int NOT NULL DEFAULT 0,
  sent_at timestamptz,
  PRIMARY KEY (broadcast_id, email)
);
CREATE INDEX IF NOT EXISTS broadcast_deliveries_queued ON broadcast_deliveries(broadcast_id) WHERE state = 'queued';
