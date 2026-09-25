-- A seller can change their link. Old links keep forwarding to the new one and
-- stay reserved for that seller, so nobody else can pick up a link people have
-- already shared and pass themselves off as them.
CREATE TABLE IF NOT EXISTS handle_redirects (
  old_handle text PRIMARY KEY,
  seller_id uuid NOT NULL REFERENCES seller_profiles(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS handle_redirects_seller ON handle_redirects(seller_id, created_at);
-- Link changes are limited per month, counted from the audit trail.
CREATE INDEX IF NOT EXISTS audit_events_target_action ON audit_events(target_id, action, created_at);
