-- The buyer pays the bank transfer fee on top of the price. The attempt
-- records that fee; expected_minor is what the buyer sends (price + fee).
ALTER TABLE payment_attempts ADD COLUMN buyer_fee_minor bigint NOT NULL DEFAULT 0 CHECK (buyer_fee_minor >= 0);
ALTER TABLE payment_attempts ADD CONSTRAINT payment_attempts_buyer_fee_bounds CHECK (buyer_fee_minor < expected_minor);
