-- Sellers open for bookings once the bank confirms their payout account.
-- An operator's hold is now its own state ('held'), so it is never undone by
-- a seller changing their bank account.
UPDATE seller_profiles sp SET readiness_state='ready'
WHERE readiness_state='incomplete' AND EXISTS (SELECT 1 FROM seller_payout_accounts a WHERE a.seller_id=sp.id);
