-- Authenticator replay protection: remember the last accepted TOTP time step.
ALTER TABLE admin_mfa ADD COLUMN IF NOT EXISTS last_totp_step bigint;

-- A verified charge whose offer was withdrawn, declined or already converted
-- is recorded for review instead of failing the payment pipeline.
ALTER TABLE payment_exceptions DROP CONSTRAINT IF EXISTS payment_exceptions_kind_check;
ALTER TABLE payment_exceptions ADD CONSTRAINT payment_exceptions_kind_check CHECK (kind IN (
  'late_payment','duplicate_charge','wrong_amount','wrong_currency','provider_dispute',
  'settlement_mismatch','provider_reversal','payment_without_slot','offer_conflict'
));

-- Only charge.success events are verified by the worker. Earlier non-charge,
-- non-case events that were queued or failed are retained but marked ignored.
UPDATE provider_events
SET state='ignored', claimed_at=NULL
WHERE state IN ('queued','retry','processing','failed')
  AND COALESCE(event_type,'') <> 'charge.success'
  AND COALESCE(event_type,'') NOT LIKE 'charge.dispute.%'
  AND COALESCE(event_type,'') NOT LIKE 'refund.%';
