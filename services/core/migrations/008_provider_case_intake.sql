CREATE TABLE provider_cases (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  provider text NOT NULL,
  environment text NOT NULL,
  provider_case_id text NOT NULL,
  payment_attempt_id uuid REFERENCES payment_attempts(id),
  provider_reference text NOT NULL,
  case_type text NOT NULL CHECK (case_type IN ('dispute','refund')),
  state text NOT NULL CHECK (state IN ('open','awaiting_provider','resolved','unknown')),
  amount_minor bigint CHECK (amount_minor IS NULL OR amount_minor >= 0),
  currency char(3),
  provider_deadline timestamptz,
  last_event_type text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(provider,environment,provider_case_id)
);
CREATE INDEX provider_cases_open ON provider_cases(updated_at DESC) WHERE state IN ('open','awaiting_provider','unknown');

CREATE TABLE settlement_import_rows (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  import_id uuid NOT NULL,
  provider text NOT NULL,
  environment text NOT NULL,
  provider_reference text NOT NULL,
  settlement_reference text,
  amount_minor bigint NOT NULL CHECK (amount_minor >= 0),
  currency char(3) NOT NULL,
  settled_at timestamptz NOT NULL,
  reconciliation_state text NOT NULL CHECK (reconciliation_state IN ('matched','unmatched','amount_mismatch','currency_mismatch','duplicate')),
  settlement_item_id uuid REFERENCES settlement_items(id),
  received_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(provider,environment,import_id,provider_reference)
);
CREATE INDEX settlement_import_review ON settlement_import_rows(received_at DESC) WHERE reconciliation_state<>'matched';
