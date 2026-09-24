ALTER TABLE email_challenges
  DROP CONSTRAINT IF EXISTS email_challenges_purpose_check;

ALTER TABLE email_challenges
  ADD CONSTRAINT email_challenges_purpose_check
  CHECK (purpose IN ('login','claim','guest_access','offer','guest_booking','guest_offer'));

CREATE INDEX IF NOT EXISTS sessions_guest_scope_idx
  ON sessions (expires_at)
  WHERE guest_scope IS NOT NULL AND revoked_at IS NULL;
