ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS provider_transaction_id text;
ALTER TABLE payment_attempts ALTER COLUMN booking_id DROP NOT NULL;
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS quote_id uuid REFERENCES quotes(id);
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS authorization_url text;
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS access_code text;
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS last_verified_at timestamptz;
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS approved_fee_minor bigint;
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS fee_basis_points integer;
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS seller_subaccount_code text;
ALTER TABLE payment_attempts ADD CONSTRAINT payment_attempts_approved_fee_bounds CHECK (approved_fee_minor IS NULL OR (approved_fee_minor >= 0 AND approved_fee_minor <= expected_minor));
ALTER TABLE payment_attempts ADD CONSTRAINT payment_attempts_fee_basis_points_bounds CHECK (fee_basis_points IS NULL OR fee_basis_points BETWEEN 0 AND 500);
ALTER TABLE payment_allocations ADD COLUMN IF NOT EXISTS fee_basis_points integer NOT NULL DEFAULT 500 CHECK (fee_basis_points BETWEEN 0 AND 500);
ALTER TABLE payment_allocations ADD CONSTRAINT payment_allocations_processor_cost_nonnegative CHECK (processor_cost_minor >= 0);
ALTER TABLE seller_profiles ADD COLUMN IF NOT EXISTS paystack_subaccount_code text;
CREATE UNIQUE INDEX IF NOT EXISTS payment_attempt_provider_transaction ON payment_attempts(provider,environment,provider_transaction_id) WHERE provider_transaction_id IS NOT NULL;

ALTER TABLE provider_events ADD COLUMN IF NOT EXISTS event_type text;
ALTER TABLE provider_events ADD COLUMN IF NOT EXISTS provider_reference text;
ALTER TABLE provider_events ADD COLUMN IF NOT EXISTS processing_attempts integer NOT NULL DEFAULT 0;
ALTER TABLE provider_events ADD COLUMN IF NOT EXISTS last_error_code text;
ALTER TABLE provider_events ADD COLUMN IF NOT EXISTS minimal_payload jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE provider_events ADD COLUMN IF NOT EXISTS next_attempt_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE provider_events ADD COLUMN IF NOT EXISTS claimed_at timestamptz;

CREATE TABLE ledger_accounts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_code text NOT NULL,
  currency char(3) NOT NULL,
  scope_id uuid,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ledger_accounts_identity ON ledger_accounts(account_code,currency,COALESCE(scope_id,'00000000-0000-0000-0000-000000000000'::uuid));
CREATE TABLE ledger_journals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_type text NOT NULL,
  source_id text NOT NULL,
  currency char(3) NOT NULL,
  description text NOT NULL,
  request_digest bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(source_type,source_id)
);
CREATE TABLE ledger_entries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  journal_id uuid NOT NULL REFERENCES ledger_journals(id),
  account_id uuid NOT NULL REFERENCES ledger_accounts(id),
  side text NOT NULL CHECK (side IN ('debit','credit')),
  amount_minor bigint NOT NULL CHECK (amount_minor > 0),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ledger_entries_journal ON ledger_entries(journal_id);
CREATE FUNCTION prevent_ledger_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'ledger history is append-only'; END $$;
CREATE TRIGGER ledger_journals_append_only BEFORE UPDATE OR DELETE ON ledger_journals FOR EACH ROW EXECUTE FUNCTION prevent_ledger_mutation();
CREATE TRIGGER ledger_entries_append_only BEFORE UPDATE OR DELETE ON ledger_entries FOR EACH ROW EXECUTE FUNCTION prevent_ledger_mutation();

CREATE TABLE payment_exceptions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  booking_id uuid REFERENCES bookings(id),
  payment_attempt_id uuid REFERENCES payment_attempts(id),
  kind text NOT NULL CHECK (kind IN ('late_payment','duplicate_charge','wrong_amount','wrong_currency','provider_dispute','settlement_mismatch','provider_reversal','payment_without_slot')),
  state text NOT NULL DEFAULT 'open' CHECK (state IN ('open','investigating','awaiting_provider','resolved')),
  provider_case_reference text,
  provider_deadline timestamptz,
  amount_minor bigint,
  currency char(3),
  reason text NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(evidence)='object'),
  resolution text,
  created_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz
);
CREATE INDEX payment_exceptions_open ON payment_exceptions(created_at) WHERE state <> 'resolved';
CREATE UNIQUE INDEX payment_exceptions_attempt_kind ON payment_exceptions(payment_attempt_id,kind) WHERE payment_attempt_id IS NOT NULL;
