-- Phase 6: profile photos in object storage (Cloudflare R2), account
-- deletion under the Nigeria Data Protection Act, and retention support.

-- Photos may live in the bucket (avatar_key) instead of the database (avatar_data).
ALTER TABLE seller_profiles ADD COLUMN avatar_key text;
ALTER TABLE seller_profiles DROP CONSTRAINT seller_avatar_pair;
ALTER TABLE seller_profiles ADD CONSTRAINT seller_avatar_pair CHECK (
  (avatar_mime IS NULL AND avatar_data IS NULL AND avatar_key IS NULL) OR
  (avatar_mime IN ('image/png','image/jpeg') AND (
    (avatar_data IS NOT NULL AND avatar_key IS NULL AND octet_length(avatar_data) <= 524288) OR
    (avatar_data IS NULL AND avatar_key ~ '^avatars/[0-9a-f-]{36}/[0-9a-f]{32}\.(png|jpg)$')
  ))
);

-- Deleted accounts keep their ID (financial and audit records point at it)
-- but lose every identifier. status: active | restricted | deleted.
ALTER TABLE users ADD COLUMN deleted_at timestamptz;
ALTER TABLE users ADD CONSTRAINT users_deleted_shape CHECK ((status = 'deleted') = (deleted_at IS NOT NULL));

-- A deleted person's link stays unclaimable for a while, so nobody can pick it
-- up and pass themselves off as them to people who still have the link.
CREATE TABLE handle_holds (
  handle text PRIMARY KEY,
  held_until timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Data export requests, rate limited per person.
CREATE TABLE data_requests (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id),
  kind text NOT NULL CHECK (kind IN ('export', 'deletion')),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX data_requests_user ON data_requests(user_id, kind, created_at DESC);

-- Retention sweeps look these up by age.
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS email_challenges_expiry ON email_challenges(expires_at);
CREATE INDEX IF NOT EXISTS product_events_received ON product_events(received_at);
CREATE INDEX IF NOT EXISTS idempotency_records_created ON idempotency_records(created_at);
