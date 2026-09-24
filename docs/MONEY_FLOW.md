# Money flow and safety

## The model: collect, hold, pay out

1. **Collect.** The buyer pays WantMyTime the full price through Kora. Bank transfer is the default: WantMyTime shows a one-off account number (a Kora dynamic virtual account) on its own checkout page, the buyer transfers the exact price from their bank app, and the page confirms the booking as soon as the money arrives. Cards are switched off (`APPROVED_PAYMENT_CHANNELS=bank_transfer`); the code for a card fallback through Kora's hosted card page stays in place for when they are turned on. The money lands in the platform's Kora balance; there is no split to the seller.
2. **Hold.** The seller's share is recorded as owed to them (`seller_payable`) and a `seller_payouts` row is scheduled. Nothing is sent before the session.
3. **Dispute window.** The buyer can report a problem or a no-show until `DISPUTE_WINDOW_MINUTES` (default 120) after the session ends. After that, the booking page and the API refuse new buyer reports, and no refund can be requested by the buyer.
4. **Pay out.** At `PAYOUT_DELAY_MINUTES` (default 180) after the end, the lifecycle worker (every minute) releases the payout if nothing holds it, and sends a transfer to the seller's verified bank account.

Reschedules need no bookkeeping: the release time is always computed from the booking's current start and length.

### What holds a payout

Worked out from the booking at release time (`payoutHoldSQL`):

- an open problem reported by the buyer;
- an open or disputed report that the seller didn't join;
- an open cancellation request to WantMyTime;
- a refund whose seller share comes out of this payout and that has failed (it must be retried or recorded first).

A problem raised by the seller does not hold their own payout. An operator resolves a problem from the booking record in Operations, optionally refunding part or all of the payment; the rest of the payout is released on the next worker cycle. Both people are emailed the outcome.

### Amount paid

At release the amount is fixed: `entitlement − seller share of refunds taken from this payout`. If that is zero (for example the seller cancelled, or a seller no-show stood), the payout is `cancelled` and nothing is sent. If the seller still owes money for a refund made after an earlier payout, up to `REFUND_RECOVERY_MAX_BPS` of the amount (default half) is kept to repay it, oldest debt first; the rest is transferred.

### Sending money (no float)

- Kora sits behind two small seams: collection (`kora.go`: start a transfer or card checkout, look a charge up, refund, verify webhooks) and payouts (the `payoutProvider` interface in `payout_provider.go`: bank list by country, account-name check, transfer, transfer lookup). A provider for another country plugs in there.
- Each attempt has its own reference (`aside-payout-<booking>`, then `-g2`, `-g3` after an operator retry). Before sending, the worker always looks the reference up, so a transfer that went out before a crash or a timeout is adopted, never sent twice.
- **When the money is there.** Kora settles bank transfers into the balance instantly, so a transfer-paid booking pays out at the 3-hour mark. Cards settle the next working day, so a card-paid payout also waits until `funds_available_at` (payment time + `CARD_SETTLEMENT_HOURS`, default 24, moved past weekends). That keeps one buyer's card money from being paid out with another buyer's transfer money before it has arrived. If Kora still refuses a transfer for insufficient balance, the payout does not fail: it waits and retries every 15 minutes. WantMyTime never pre-funds (tops up) payouts.
- Kora needs the bank code and account number on every transfer, so the account number is kept encrypted (AES-GCM, bound to the seller, key `PAYOUT_ACCOUNT_ENCRYPTION_KEY`) and copied onto the payout when it is released.
- Pending transfers are checked every 10 minutes; `transfer.*` webhooks bring the check forward. A 5xx from Kora is never taken as a failure: the next attempt looks the reference up first.
- A refused or failed transfer marks the payout `failed` and raises the `payouts_failed` alert.
- A transfer that fails after it was recorded as paid (`transfer.failed`, confirmed with Kora before anything changes) marks the payout `failed` and reverses its journal. An operator retries it from Operations › Payouts after the seller fixes their account; the retry goes to the seller's current account under a new reference.
- `PAYOUTS_PAUSED=true` stops all transfers; payouts wait and go out when it is lifted.

### Seller bank accounts

Sellers add a bank account under Settings › Payouts. The account number is checked with the bank and the account name shown before saving; the account number is stored encrypted, with a keyed fingerprint to tell a changed account from a re-save; only the bank name, last four digits and account name are readable. Each account records its country and currency; Nigeria (NGN, 10-digit NUBAN) is the only country switched on so far (`payoutCountries`). Replacing an account starts a 24-hour hold before payouts go to the new one, so a stolen session cannot quietly redirect money. Lookups are limited to 20 an hour per person and every change is audited. Operators mark a seller ready for paid bookings only once a bank account is on file, and checkout refuses sellers without one.

## Ledger

| Event | Debit | Credit |
|---|---|---|
| Verified payment | `provider_receivable` (gross − processor fee), `processor_fee_expense` | `platform_fee_revenue` (fee), `seller_payable` (entitlement) |
| Refund, payout not yet sent | `platform_fee_revenue` (platform share), `seller_payable` (seller share) | `provider_receivable` |
| Refund after payout | `platform_fee_revenue`, `seller_receivable` (seller share; a `seller_recoveries` row) | `provider_receivable` |
| Debt repaid from a payout | `seller_payable` | `seller_receivable` |
| Payout sent | `seller_payable` (transferred amount), `processor_fee_expense` (transfer fee) | `provider_receivable` |
| Payout returned by bank | `provider_receivable` | `seller_payable` |

Every journal balances. After a booking is fully settled, the seller's `seller_payable` for it nets to zero.

## Required approved calculation

For gross `G` in minor units and approved fee rate `b` basis points, `0 <= b <= 500`, `F=floor(G*b/10000)`, `S=G-F`, and `G=F+S`. Processor cost must fit within the approved deduction (or match the approved international-card schedule, which the platform absorbs). Never reduce seller entitlement or add an undisclosed buyer fee.

## Refunds

**When refunds happen:**
- A buyer cancels: the booking's frozen policy decides the amount.
- A seller cancels, or a seller no-show stands: the buyer gets a full refund.
- An operator resolves a reported problem with a refund, or refunds a booking directly: any amount up to the price, with a reason, audited.

**Shares:** each refund `R` is split in proportion to the original charge: platform share `floor(R*F/G)`, seller share the rest. One live refund is allowed per booking. The provider does not return its processing fee on refunds; the platform absorbs it.

**Whose money:** each refund records `seller_liability`. While the payout is still `scheduled`, it is `payable`: the seller share simply reduces the payout. Once the payout has been released, it is `receivable`: the seller owes it back and it is recovered from later payouts as above.

**Sending refunds:**
- With `REFUNDS_ENABLED=true`, refunds go to Kora under a reference derived from the WantMyTime refund (`aside-refund-<id>`). Every attempt looks that reference up first, so a refund whose response was lost is adopted rather than duplicated.
- Refund webhooks prompt an immediate status check. Anything still pending is polled every 30 minutes.
- A refund Kora refuses (4xx), or one that fails every retry, stops as `failed` and raises an alert.
- With automatic refunds off, operators approve each one, or refund in the Kora dashboard and record it. Recording is only allowed for refunds WantMyTime has not sent, so a buyer cannot be refunded twice.

## Other safeguards

- Browser redirects are not proof of payment. The checkout page and the return route verify the charge with Kora's charge lookup. Webhooks are signed (HMAC-SHA256 of the `data` object with the secret key). Signed webhook requests are size-limited, signature-checked, minimized, durably queued and independently verified by a retrying worker.
- Expired slots, mismatched charges and processor cost beyond the approved deduction become payment exceptions and do not create a paid booking.
- **Transfers of the wrong amount.** Only the amount Kora accepts for the charge counts. Keep Kora's dashboard defaults: a short transfer is returned to the buyer and the booking stays unpaid (the checkout page tells the buyer to send the exact amount); an overpayment has the excess returned.
- **Paying twice.** A buyer who starts a transfer and then pays by card gets two charges for one quote. The first to arrive books the time; the second becomes a `duplicate_charge` exception to refund.
- **Late transfers.** The time is held until 10 minutes after the transfer account expires (at most 3 hours). A payment that arrives after the hold has lapsed becomes a `payment_without_slot` exception to refund.
- Alerts: `payouts_failed` (critical), `payouts_late` (a payout unpaid a day after it was due and not held), `problems_open` (buyer problems holding payouts), plus the refund and no-show alerts.
- Live collection is not authorized by this code alone: live keys, approval ID, fee schedules, legal and commercial approval and owner deployment controls remain launch gates.
