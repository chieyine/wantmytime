-- Escrow payouts through Kora. Aside collects the full payment (bank transfer
-- first, card as a fallback), holds it through a short dispute window after
-- the session and then transfers the seller's share to their bank account.

-- The bank account a seller is paid into. The account number is encrypted
-- (bound to the seller); only its last four digits are readable.
CREATE TABLE IF NOT EXISTS seller_payout_accounts (
  seller_id uuid PRIMARY KEY REFERENCES seller_profiles(id),
  provider text NOT NULL,
  country char(2) NOT NULL DEFAULT 'NG',
  bank_code text NOT NULL CHECK (length(bank_code) BETWEEN 2 AND 20),
  bank_name text NOT NULL CHECK (length(bank_name) BETWEEN 1 AND 120),
  account_last4 char(4) NOT NULL CHECK (account_last4 ~ '^[0-9]{4}$'),
  account_name text NOT NULL CHECK (length(account_name) BETWEEN 1 AND 160),
  account_sealed bytea NOT NULL,
  -- Keyed hash of the account, to tell a changed account from a re-save.
  account_fingerprint text NOT NULL,
  currency char(3) NOT NULL DEFAULT 'NGN',
  verified_at timestamptz NOT NULL DEFAULT now(),
  -- A replaced account is not paid into until this time, so a stolen
  -- session cannot quietly redirect money that is about to be paid out.
  usable_from timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- One payout per paid booking. Whether it is due is worked out from the
-- booking itself (end time, open problems, refunds), so a reschedule or a
-- dispute needs no bookkeeping here.
--   scheduled   waiting for the release time, or on hold
--   processing  amount fixed and a transfer started (or waiting for funds)
--   paid        the provider confirmed the transfer
--   failed      the provider refused or reversed it; an operator can retry
--   cancelled   nothing left to pay (fully refunded)
CREATE TABLE IF NOT EXISTS seller_payouts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  booking_id uuid NOT NULL UNIQUE REFERENCES bookings(id),
  seller_id uuid NOT NULL REFERENCES seller_profiles(id),
  provider text NOT NULL,
  entitlement_minor bigint NOT NULL CHECK (entitlement_minor >= 0),
  amount_minor bigint CHECK (amount_minor IS NULL OR amount_minor >= 0),
  recovery_minor bigint NOT NULL DEFAULT 0 CHECK (recovery_minor >= 0),
  fee_minor bigint NOT NULL DEFAULT 0 CHECK (fee_minor >= 0),
  currency char(3) NOT NULL,
  state text NOT NULL DEFAULT 'scheduled' CHECK (state IN ('scheduled', 'processing', 'paid', 'failed', 'cancelled')),
  reference text NOT NULL UNIQUE CHECK (reference ~ '^[a-z0-9_-]{16,50}$'),
  -- Where the money goes, fixed when the payout is released.
  bank_code text,
  account_sealed bytea,
  account_name text,
  account_last4 char(4),
  bank_name text,
  provider_transfer_code text,
  -- When the buyer's money is in the balance: at once for bank transfers,
  -- a working day later for cards.
  funds_available_at timestamptz NOT NULL DEFAULT now(),
  attempts integer NOT NULL DEFAULT 0,
  generation integer NOT NULL DEFAULT 1,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  last_error text,
  released_at timestamptz,
  paid_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (amount_minor IS NULL OR recovery_minor <= amount_minor)
);
CREATE INDEX IF NOT EXISTS seller_payouts_work ON seller_payouts(next_attempt_at) WHERE state IN ('scheduled', 'processing');
CREATE INDEX IF NOT EXISTS seller_payouts_seller ON seller_payouts(seller_id, created_at DESC);

-- Whether a refund's seller share comes out of a payout that has not gone
-- yet ('payable') or was already paid and is recovered later ('receivable').
ALTER TABLE refunds ADD COLUMN IF NOT EXISTS seller_liability text NOT NULL DEFAULT 'receivable';
ALTER TABLE refunds DROP CONSTRAINT IF EXISTS refunds_seller_liability_check;
ALTER TABLE refunds ADD CONSTRAINT refunds_seller_liability_check CHECK (seller_liability IN ('payable', 'receivable'));
ALTER TABLE refunds DROP CONSTRAINT IF EXISTS refunds_reason_check;
ALTER TABLE refunds ADD CONSTRAINT refunds_reason_check CHECK (reason IN ('buyer_cancelled', 'seller_cancelled', 'seller_no_show', 'operator', 'problem_upheld'));

-- The subaccount split never went live, so its columns go. Debts from
-- refunds made after a payout are now repaid from later payouts
-- (seller_payouts.recovery_minor), not from checkouts.
ALTER TABLE payment_allocations DROP CONSTRAINT IF EXISTS payment_allocations_split_identity;
ALTER TABLE payment_allocations DROP COLUMN IF EXISTS recovery_minor;
ALTER TABLE payment_allocations ADD CONSTRAINT payment_allocations_split_identity CHECK (gross_minor = deduction_minor + seller_entitlement_minor);
ALTER TABLE payment_attempts DROP COLUMN IF EXISTS recovery_minor;
ALTER TABLE payment_attempts DROP COLUMN IF EXISTS seller_subaccount_code;
ALTER TABLE seller_profiles DROP COLUMN IF EXISTS paystack_subaccount_code;

-- Checkout: how the buyer paid, and the one-off transfer account to show
-- again if they reload the page.
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS channel text;
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS transfer_details jsonb;
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS instructions_expire_at timestamptz;
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS paid_minor bigint;

-- The provider is Kora. Fee schedules approved for Paystack no longer apply.
DELETE FROM provider_fee_schedules WHERE provider = 'paystack';

-- Who raised a problem on a booking; only the buyer's holds the payout.
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS issue_reported_by text CHECK (issue_reported_by IS NULL OR issue_reported_by IN ('buyer', 'seller'));
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS issue_resolution text;

ALTER TABLE notification_outbox DROP CONSTRAINT IF EXISTS notification_outbox_kind_check;
ALTER TABLE notification_outbox ADD CONSTRAINT notification_outbox_kind_check
  CHECK (kind IN (
    'booking_confirmed_buyer','booking_confirmed_seller','meeting_link_ready_buyer',
    'booking_reminder_24h_buyer','booking_reminder_24h_seller',
    'booking_reminder_1h_buyer','booking_reminder_1h_seller',
    'meeting_link_due_24h_seller','meeting_link_due_2h_seller',
    'reschedule_accepted_buyer','reschedule_accepted_seller',
    'reschedule_requested_buyer','reschedule_requested_seller',
    'reschedule_declined_buyer','reschedule_declined_seller',
    'cancellation_requested_buyer','cancellation_requested_seller',
    'cancellation_reviewed_buyer','cancellation_reviewed_seller',
    'offer_received_seller','offer_countered_buyer',
    'offer_accepted_buyer','offer_accepted_seller',
    'offer_declined_buyer','offer_declined_seller',
    'offer_withdrawn_buyer','offer_withdrawn_seller',
    'booking_cancelled_buyer','booking_cancelled_seller',
    'refund_processed_buyer',
    'no_show_reported_buyer','no_show_reported_seller',
    'no_show_resolved_buyer','no_show_resolved_seller',
    'review_request_buyer','review_received_seller',
    'payout_sent_seller','problem_reported_seller',
    'problem_resolved_buyer','problem_resolved_seller'
  ));
