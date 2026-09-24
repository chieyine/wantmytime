CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
  id uuid PRIMARY KEY,
  display_name text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  timezone text NOT NULL DEFAULT 'Africa/Lagos',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE user_identities (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id),
  type text NOT NULL,
  normalized_identifier text NOT NULL,
  verified_at timestamptz,
  UNIQUE(type, normalized_identifier)
);
CREATE TABLE sessions (
  id uuid PRIMARY KEY,
  token_hash bytea NOT NULL UNIQUE,
  user_id uuid REFERENCES users(id),
  guest_scope jsonb,
  expires_at timestamptz NOT NULL,
  mfa_verified_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE seller_profiles (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL UNIQUE REFERENCES users(id),
  handle text NOT NULL UNIQUE,
  mode text NOT NULL CHECK (mode IN ('fixed','offer')),
  publication_state text NOT NULL DEFAULT 'draft',
  readiness_state text NOT NULL DEFAULT 'incomplete',
  timezone text NOT NULL DEFAULT 'Africa/Lagos',
  minimum_notice_minutes smallint NOT NULL DEFAULT 60 CHECK (minimum_notice_minutes BETWEEN 0 AND 10080),
  booking_horizon_days smallint NOT NULL DEFAULT 30 CHECK (booking_horizon_days BETWEEN 1 AND 365),
  buffer_minutes smallint NOT NULL DEFAULT 0 CHECK (buffer_minutes BETWEEN 0 AND 240),
  paused boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE pricing_versions (
  id uuid PRIMARY KEY,
  seller_id uuid NOT NULL REFERENCES seller_profiles(id),
  currency char(3) NOT NULL CHECK (currency='NGN'),
  base_30_minor bigint NOT NULL CHECK (base_30_minor >= 0),
  durations integer[] NOT NULL,
  fee_basis_points smallint NOT NULL CHECK (fee_basis_points BETWEEN 0 AND 500),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE email_challenges (
  id uuid PRIMARY KEY,
  normalized_email text NOT NULL,
  purpose text NOT NULL CHECK (purpose IN ('login','claim','guest_access','offer')),
  code_hash bytea NOT NULL,
  expires_at timestamptz NOT NULL,
  attempts smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX email_challenges_pending_idx ON email_challenges(normalized_email,purpose,expires_at) WHERE consumed_at IS NULL;
CREATE TABLE availability_windows (
  id uuid PRIMARY KEY,
  seller_id uuid NOT NULL REFERENCES seller_profiles(id),
  weekday smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
  local_start time NOT NULL,
  local_end time NOT NULL CHECK (local_end > local_start),
  timezone text NOT NULL
);
CREATE INDEX availability_windows_seller_weekday_idx ON availability_windows(seller_id,weekday,local_start);
CREATE TABLE availability_overrides (
  id uuid PRIMARY KEY,
  seller_id uuid NOT NULL REFERENCES seller_profiles(id),
  local_date date NOT NULL,
  closed boolean NOT NULL DEFAULT false,
  replacement_windows jsonb,
  UNIQUE(seller_id,local_date)
);
CREATE TABLE quotes (
  id uuid PRIMARY KEY,
  seller_id uuid NOT NULL REFERENCES seller_profiles(id),
  buyer_user_id uuid NOT NULL REFERENCES users(id),
  pricing_version_id uuid NOT NULL REFERENCES pricing_versions(id),
  buyer_name text NOT NULL,
  duration_minutes integer NOT NULL CHECK (duration_minutes IN (15,30,60)),
  starts_at timestamptz NOT NULL,
  gross_minor bigint NOT NULL CHECK (gross_minor > 0),
  currency char(3) NOT NULL DEFAULT 'NGN' CHECK (currency='NGN'),
  state text NOT NULL CHECK (state IN ('held','expired','converted')),
  expires_at timestamptz NOT NULL,
  idempotency_key text NOT NULL,
  request_digest bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(buyer_user_id,idempotency_key)
);
CREATE TABLE offers (
  id uuid PRIMARY KEY,
  seller_id uuid NOT NULL REFERENCES seller_profiles(id),
  buyer_user_id uuid NOT NULL REFERENCES users(id),
  buyer_name text NOT NULL,
  buyer_email text NOT NULL,
  duration_minutes integer NOT NULL CHECK (duration_minutes > 0),
  state text NOT NULL,
  version integer NOT NULL DEFAULT 1,
  expires_at timestamptz NOT NULL,
  checkout_expires_at timestamptz,
  idempotency_key text NOT NULL,
  request_digest bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX offers_buyer_idempotency_idx ON offers(buyer_user_id,idempotency_key);
ALTER TABLE quotes ADD COLUMN offer_id uuid REFERENCES offers(id);
CREATE TABLE offer_versions (
  id uuid PRIMARY KEY,
  offer_id uuid NOT NULL REFERENCES offers(id),
  version integer NOT NULL,
  amount_minor bigint NOT NULL CHECK (amount_minor > 0),
  actor text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(offer_id,version)
);
CREATE TABLE slot_reservations (
  id uuid PRIMARY KEY,
  seller_id uuid NOT NULL REFERENCES seller_profiles(id),
  occupied_from timestamptz NOT NULL,
  occupied_to timestamptz NOT NULL,
  occupied_range tstzrange GENERATED ALWAYS AS (tstzrange(occupied_from,occupied_to,'[)')) STORED,
  active boolean NOT NULL DEFAULT true,
  reservation_kind text NOT NULL CHECK (reservation_kind IN ('hold','booking')),
  quote_id uuid UNIQUE REFERENCES quotes(id),
  expires_at timestamptz,
  CHECK (occupied_to > occupied_from),
  CHECK (reservation_kind <> 'hold' OR expires_at IS NOT NULL),
  EXCLUDE USING gist (seller_id WITH =, occupied_range WITH &&) WHERE (active)
);
CREATE TABLE bookings (
  id uuid PRIMARY KEY,
  quote_id uuid UNIQUE REFERENCES quotes(id),
  seller_id uuid NOT NULL REFERENCES seller_profiles(id),
  buyer_user_id uuid NOT NULL REFERENCES users(id),
  buyer_name text NOT NULL,
  guest_email text NOT NULL,
  duration_minutes integer NOT NULL CHECK (duration_minutes > 0),
  starts_at timestamptz NOT NULL,
  gross_minor bigint NOT NULL CHECK (gross_minor > 0),
  currency char(3) NOT NULL DEFAULT 'NGN',
  state text NOT NULL,
  payment_state text NOT NULL,
  quote_snapshot jsonb NOT NULL DEFAULT '{}',
  meeting_url bytea,
  meeting_deadline timestamptz NOT NULL,
  meeting_ready_at timestamptz,
  issue_reason text,
  issue_created_at timestamptz,
  issue_resolved_at timestamptz,
  buyer_completed_at timestamptz,
  seller_completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE slot_reservations ADD COLUMN booking_id uuid UNIQUE REFERENCES bookings(id);
ALTER TABLE offers ADD COLUMN converted_booking_id uuid UNIQUE REFERENCES bookings(id);
CREATE TABLE payment_attempts (
  id uuid PRIMARY KEY,
  booking_id uuid NOT NULL REFERENCES bookings(id),
  provider text NOT NULL,
  environment text NOT NULL,
  merchant_reference text NOT NULL,
  expected_minor bigint NOT NULL CHECK (expected_minor > 0),
  currency char(3) NOT NULL,
  canonical_state text NOT NULL,
  UNIQUE(provider,environment,merchant_reference)
);
CREATE TABLE payment_allocations (
  id uuid PRIMARY KEY,
  booking_id uuid NOT NULL UNIQUE REFERENCES bookings(id),
  gross_minor bigint NOT NULL,
  deduction_minor bigint NOT NULL CHECK (deduction_minor >= 0 AND deduction_minor <= gross_minor AND deduction_minor <= floor(gross_minor::numeric * 500 / 10000)),
  seller_entitlement_minor bigint NOT NULL,
  processor_cost_minor bigint NOT NULL DEFAULT 0,
  settlement_route text NOT NULL CHECK (settlement_route IN ('direct_split','approved_transfer')),
  settlement_route_snapshot jsonb NOT NULL DEFAULT '{}',
  CHECK (gross_minor = deduction_minor + seller_entitlement_minor)
);
CREATE TABLE settlement_items (
  id uuid PRIMARY KEY,
  allocation_id uuid NOT NULL UNIQUE REFERENCES payment_allocations(id),
  route text NOT NULL CHECK (route IN ('direct_split','approved_transfer')),
  state text NOT NULL,
  provider_reference text,
  UNIQUE(id,allocation_id,route)
);
CREATE FUNCTION enforce_settlement_route_match() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE allocation_route text;
BEGIN
  SELECT settlement_route INTO allocation_route FROM payment_allocations WHERE id=NEW.allocation_id;
  IF allocation_route IS DISTINCT FROM NEW.route THEN
    RAISE EXCEPTION 'settlement route must match immutable payment allocation route';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER settlement_route_match BEFORE INSERT OR UPDATE OF allocation_id,route ON settlement_items
FOR EACH ROW EXECUTE FUNCTION enforce_settlement_route_match();
CREATE FUNCTION prevent_route_change_after_collection() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.settlement_route IS DISTINCT FROM OLD.settlement_route THEN
    RAISE EXCEPTION 'settlement route is immutable after allocation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER immutable_allocation_route BEFORE UPDATE OF settlement_route ON payment_allocations
FOR EACH ROW EXECUTE FUNCTION prevent_route_change_after_collection();
CREATE TABLE settlement_confirmations (
  id uuid PRIMARY KEY,
  settlement_item_id uuid NOT NULL REFERENCES settlement_items(id),
  amount_minor bigint NOT NULL CHECK (amount_minor >= 0),
  confirmed_at timestamptz NOT NULL,
  source text NOT NULL,
  reconciliation_state text NOT NULL
);
CREATE TABLE provider_events (
  id uuid PRIMARY KEY,
  provider text NOT NULL,
  environment text NOT NULL,
  event_digest bytea NOT NULL,
  raw_body_hash bytea NOT NULL,
  state text NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(provider,environment,event_digest)
);
CREATE TABLE audit_events (
  id uuid PRIMARY KEY,
  actor_id uuid REFERENCES users(id),
  action text NOT NULL,
  target_id uuid,
  reason text,
  safe_summary jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION prevent_audit_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit events are append-only';
END $$;
CREATE TRIGGER audit_events_append_only BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW EXECUTE FUNCTION prevent_audit_mutation();
CREATE TABLE admin_grants (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id),
  permission text NOT NULL,
  granted_by uuid NOT NULL REFERENCES users(id),
  granted_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz,
  UNIQUE(user_id,permission,granted_at)
);
CREATE UNIQUE INDEX admin_grants_active_permission_idx ON admin_grants(user_id,permission) WHERE revoked_at IS NULL;
CREATE TABLE admin_mfa (
  user_id uuid PRIMARY KEY REFERENCES users(id),
  encrypted_secret bytea NOT NULL,
  failed_attempts smallint NOT NULL DEFAULT 0 CHECK (failed_attempts BETWEEN 0 AND 5),
  locked_until timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE provider_fee_schedules (
  id uuid PRIMARY KEY,
  provider text NOT NULL,
  currency char(3) NOT NULL,
  channel text NOT NULL,
  percent_bps integer NOT NULL CHECK (percent_bps >= 0),
  fixed_minor bigint NOT NULL CHECK (fixed_minor >= 0),
  cap_minor bigint,
  effective_from timestamptz NOT NULL,
  approved_at timestamptz,
  approved_by uuid REFERENCES users(id)
);
CREATE TABLE idempotency_records (
  id uuid PRIMARY KEY,
  actor_scope text NOT NULL,
  operation text NOT NULL,
  idempotency_key text NOT NULL,
  request_digest bytea NOT NULL,
  response_status integer,
  response_body jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(actor_scope,operation,idempotency_key)
);
CREATE TABLE outbox_jobs (
  id uuid PRIMARY KEY,
  job_type text NOT NULL,
  dedupe_key text NOT NULL UNIQUE,
  payload_ref jsonb NOT NULL,
  run_at timestamptz NOT NULL DEFAULT now(),
  leased_until timestamptz,
  attempts integer NOT NULL DEFAULT 0,
  completed_at timestamptz,
  last_error_code text
);
CREATE TABLE product_events (
  id uuid PRIMARY KEY,
  event_name text NOT NULL,
  subject_hash bytea,
  environment text NOT NULL,
  props jsonb NOT NULL DEFAULT '{}',
  occurred_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE growth_relationships (
  id uuid PRIMARY KEY,
  source_user_id uuid NOT NULL REFERENCES users(id),
  child_user_id uuid NOT NULL REFERENCES users(id),
  qualifying_booking_id uuid REFERENCES bookings(id),
  attribution_method text NOT NULL,
  attributed_at timestamptz NOT NULL DEFAULT now(),
  CHECK (source_user_id <> child_user_id),
  UNIQUE(source_user_id,child_user_id,qualifying_booking_id)
);
