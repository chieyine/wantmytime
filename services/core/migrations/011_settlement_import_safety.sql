-- Earlier normalized CSV imports were operator-supplied, not authenticated
-- provider confirmations. Preserve their evidence while removing the claim
-- that those rows prove a bank settlement.
UPDATE settlement_confirmations
SET reconciliation_state='unverified_import'
WHERE source='paystack_normalized_import' AND reconciliation_state='matched';

UPDATE settlement_items si
SET state='provider_split_pending'
WHERE si.state='settled'
  AND EXISTS (SELECT 1 FROM settlement_confirmations sc WHERE sc.settlement_item_id=si.id AND sc.source='paystack_normalized_import')
  AND NOT EXISTS (SELECT 1 FROM settlement_confirmations sc WHERE sc.settlement_item_id=si.id AND sc.reconciliation_state='matched');

-- Retain every historical row, but only one can remain the canonical
-- provider-confirmed record for an item.
WITH ranked AS (
  SELECT id, row_number() OVER (PARTITION BY settlement_item_id ORDER BY confirmed_at,id) AS position
  FROM settlement_confirmations WHERE reconciliation_state='matched'
)
UPDATE settlement_confirmations sc SET reconciliation_state='duplicate_prior_confirmation'
FROM ranked WHERE sc.id=ranked.id AND ranked.position>1;

CREATE UNIQUE INDEX IF NOT EXISTS settlement_one_verified_confirmation
ON settlement_confirmations(settlement_item_id) WHERE reconciliation_state='matched';
