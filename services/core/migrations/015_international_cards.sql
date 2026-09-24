-- Where the paying card was issued, as reported by Paystack verification.
-- Used to accept foreign-card fees under the approved international schedule
-- and to report how much of the platform fee they consumed. No card number,
-- BIN or last four digits are stored.
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS card_country char(2);
ALTER TABLE payment_attempts ADD COLUMN IF NOT EXISTS card_brand text;
ALTER TABLE payment_attempts DROP CONSTRAINT IF EXISTS payment_attempts_card_country_check;
ALTER TABLE payment_attempts ADD CONSTRAINT payment_attempts_card_country_check CHECK (card_country IS NULL OR card_country ~ '^[A-Z]{2}$');
