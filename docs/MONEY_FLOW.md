# Money flow and safety

## Currencies and countries

Every seller has a country (chosen when they claim their link) and is priced, charged and paid in its currency. Nigeria (NGN) is on by default; Ghana (GHS) and Kenya (KES) are built in and switched on with `SELLER_COUNTRIES` once Kora has enabled them on the merchant account (`markets.go`). Buyers can be anywhere: they see times in their own timezone and pay in the seller's currency by that country's local method. WantMyTime takes no card payments. All amounts stay integer minor units with an ISO currency on every record; ledger journals are per currency.

| Country | Currency | Buyers pay by | Sellers are paid to | Account name check |
|---|---|---|---|---|
| Nigeria | NGN | bank transfer (default), pay with bank | bank account (10-digit NUBAN) | by the bank, before saving |
| Ghana | GHS | mobile money | bank account or mobile money wallet | typed by the seller |
| Kenya | KES | mobile money | bank account or mobile money (M-Pesa, Airtel) | typed by the seller |

Per currency: `APPROVED_PAYMENT_CHANNELS_<CUR>` (NGN also reads `APPROVED_PAYMENT_CHANNELS`), `MIN_CHARGE_MINOR_<CUR>`/`MAX_CHARGE_MINOR_<CUR>` (NGN also reads the plain names). A currency without both simply does not take payments; its sellers' pages say bookings open soon.

## The model: collect, hold, pay out

1. **Collect.** The buyer pays WantMyTime the full price through Kora. In Nigeria bank transfer is the default: WantMyTime shows a one-off account number (a Kora dynamic virtual account) on its own checkout page, the buyer transfers the exact amount from their bank app, and the page confirms the booking as soon as the money arrives. Pay with bank and mobile money open Kora's hosted payment page and come back to `/payment/return`, which confirms the booking by itself as soon as Kora does. The money lands in the platform's Kora balance; there is no split to the seller.
2. **Hold.** The seller's share is recorded as owed to them (`seller_payable`) and a `seller_payouts` row is scheduled. Nothing is sent before the session.
3. **Dispute window.** The buyer can report a problem or a no-show until `DISPUTE_WINDOW_MINUTES` (default 120) after the session ends. After that, the booking page and the API refuse new buyer reports, and no refund can be requested by the buyer.
4. **Pay out.** At `PAYOUT_DELAY_MINUTES` (default 180) after the end, the lifecycle worker (every minute) releases the payout if nothing holds it, and sends a transfer to the seller's verified bank account.

Reschedules need no bookkeeping: the release time is always computed from the booking's current start and length.

### What holds a payout

Worked out from the booking at release time (`payoutHoldSQL`):

- an open problem reported by the buyer;
- an open or disputed report that the seller didn't join;
- a refund whose seller share comes out of this payout and that has failed (it must be retried or recorded first).

A buyer's request to cancel outside the policy does not hold the payout: it goes to the seller, who can cancel (full refund) or let it stand, and it closes by itself when the booking time arrives.

A problem raised by the seller does not hold their own payout. An operator resolves a problem from the booking record in Operations, optionally refunding part or all of the payment; the rest of the payout is released on the next worker cycle. Both people are emailed the outcome.

### Amount paid

At release the amount is fixed: `entitlement − seller share of refunds taken from this payout`. If that is zero (for example the seller cancelled, or a seller no-show stood), the payout is `cancelled` and nothing is sent. If the seller still owes money for a refund made after an earlier payout, up to `REFUND_RECOVERY_MAX_BPS` of the amount (default half) is kept to repay it, oldest debt first; the rest is transferred.

### Sending money (no float)

- Kora sits behind two small seams: collection (`kora.go`: start a transfer or hosted checkout, look a charge up, refund, verify webhooks) and payouts (the `payoutProvider` interface in `payout_provider.go`: bank list by country, account-name check, transfer, transfer lookup). A provider for another country plugs in there.
- Each attempt has its own reference (`aside-payout-<booking>`, then `-g2`, `-g3` after an operator retry). Before sending, the worker always looks the reference up, so a transfer that went out before a crash or a timeout is adopted, never sent twice.
- **When the money is there.** Kora settles bank transfers into the balance instantly, so a transfer-paid booking pays out at the 3-hour mark. Pay-with-bank and mobile money payments can settle later, so those payouts also wait until `funds_available_at` (payment time + `SETTLEMENT_WAIT_HOURS`, default 24, moved past weekends). That keeps one buyer's unsettled money from being paid out with another buyer's transfer money before it has arrived. If Kora still refuses a transfer for insufficient balance, the payout does not fail: it waits and retries every 15 minutes. WantMyTime never pre-funds (tops up) payouts.
- Kora needs the bank code and account number on every transfer, so the account number is kept encrypted (AES-GCM, bound to the seller, key `PAYOUT_ACCOUNT_ENCRYPTION_KEY`) and copied onto the payout when it is released.
- Pending transfers are checked every 10 minutes; `transfer.*` webhooks bring the check forward. A 5xx from Kora is never taken as a failure: the next attempt looks the reference up first.
- A refused or failed transfer marks the payout `failed` and raises the `payouts_failed` alert.
- A transfer that fails after it was recorded as paid (`transfer.failed`, confirmed with Kora before anything changes) marks the payout `failed` and reverses its journal.
- Failed payouts are sent again without anyone stepping in: once by themselves two hours after the first failure, and again as soon as the seller saves a different payout account (once its 24-hour safety hold has passed). Each retry goes to the seller's current account under a new reference (`-g2`, `-g3`…) and is audited as `payout.auto_retry`. An operator can still retry from Operations › Payouts.
- `PAYOUTS_PAUSED=true` stops all transfers; payouts wait and go out when it is lifted.

### Seller payout accounts

Sellers add a payout account under Settings › Payouts, in their own country: a bank account, or in Ghana and Kenya a mobile money wallet (network plus phone number, stored in international form). A Nigerian bank account is checked with the bank and the account name shown before saving (Kora's resolve call takes the currency, `NGN`); elsewhere the seller types the name on the account. The account or wallet number is stored encrypted, with a keyed fingerprint to tell a changed account from a re-save; only the bank or network name, last four digits and account name are readable. Replacing an account starts a 24-hour hold before payouts go to the new one, so a stolen session cannot quietly redirect money. Lookups are limited to 20 an hour per person and every change is audited. Saving a payout account opens the seller for bookings (unless an operator has put them on hold), and checkout refuses sellers without one.

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

For gross `G` in minor units and approved fee rate `b` basis points, `0 <= b <= 500`, `F=floor(G*b/10000)`, `S=G-F`, and `G=F+S`. The processor's fee is paid by the buyer on top, so it never reduces `F` or `S`. Never reduce seller entitlement or add an undisclosed buyer fee.

**Payment fee (buyer pays, set by Kora).** WantMyTime asks Kora for the price `G` with `merchant_bears_cost=false`, and Kora adds its own current fee `f` for the buyer. A fee change at Kora is therefore picked up automatically. For a transfer, Kora quotes the exact total when it creates the account, and WantMyTime shows it with the fee split out; a hosted page (pay with bank, mobile money) shows Kora's total. Before the buyer chooses, an optional fee schedule gives an estimate. On verification `payment_attempts.buyer_fee_minor` is set to the fee Kora actually charged and `expected_minor = G + f`. The allocation uses `G` as gross: platform fee `F` and seller entitlement `S` are computed on the price alone. The journal debits `processor_fee_expense` with `f` and credits `platform_fee_revenue` with `F + f`, so the fee passes straight through.

## Refunds

**When refunds happen:**
- A buyer cancels: the booking's frozen policy decides the amount.
- A seller cancels, or a seller no-show stands: the buyer gets a full refund.
- An operator resolves a reported problem with a refund, or refunds a booking directly: any amount up to the price, with a reason, audited.

**Transfer fee on refunds:** a buyer cancellation refunds the price under the policy; the transfer fee is not returned. A seller cancellation or a seller no-show that stands returns everything, fee included, and the platform bears the fee.

**Shares:** each refund `R` is split in proportion to the original charge: platform share `floor(R*F/G)`, seller share the rest. One live refund is allowed per booking. The provider does not return its processing fee on refunds; the platform absorbs it.

**Whose money:** each refund records `seller_liability`. While the payout is still `scheduled`, it is `payable`: the seller share simply reduces the payout. Once the payout has been released, it is `receivable`: the seller owes it back and it is recovered from later payouts as above.

**Sending refunds:**
- With `REFUNDS_ENABLED=true`, refunds go to Kora under a reference derived from the WantMyTime refund (`aside-refund-<id>`). Every attempt looks that reference up first, so a refund whose response was lost is adopted rather than duplicated.
- Refund webhooks prompt an immediate status check. Anything still pending is polled every 30 minutes.
- A refund Kora refuses (4xx), or one that fails every retry, stops as `failed` and raises an alert.
- With automatic refunds off, operators approve each one, or refund in the Kora dashboard and record it. Recording is only allowed for refunds WantMyTime has not sent, so a buyer cannot be refunded twice.

## Other safeguards

- Browser redirects are not proof of payment. The checkout page and the return route verify the charge with Kora's charge lookup. Webhooks are signed (HMAC-SHA256 of the `data` object with the secret key). Signed webhook requests are size-limited, signature-checked, minimized, durably queued and independently verified by a retrying worker.
- **Payments that cannot become a booking are returned automatically.** A late transfer (the hold lapsed first) still gets its time if the time is still in the future, inside the seller's hours and not taken. If it was taken, or the seller stopped taking bookings, or the offer had closed (`payment_without_slot`, `offer_conflict`), or the buyer paid twice (`duplicate_charge`), the exception is recorded and a full refund of that payment is created at once (`reason='unbooked_payment'`, tied to the exception, not a booking; queued with `REFUNDS_ENABLED=true`, otherwise waiting for approval). The buyer is emailed when it starts and when it is sent; the exception resolves itself when the refund completes. These charges never reached the booking ledger, so nothing there is reversed.
- **Kora changes its fees.** Nothing to do: Kora adds its current fee for the buyer, and the seller's share and the 5% are unchanged.
- Wrong amounts or currencies (which Kora normally prevents by returning short and excess transfers) are recorded for review and do not create a booking.
- **Transfers of the wrong amount.** Only the amount Kora accepts for the charge counts. Keep Kora's dashboard defaults: a short transfer is returned to the buyer and the booking stays unpaid (the checkout page tells the buyer to send the exact amount); an overpayment has the excess returned.
- **Paying twice.** A buyer who starts a transfer and then pays with bank gets two charges for one quote. The first to arrive books the time; the second becomes a `duplicate_charge` exception and is refunded automatically.
- **Late transfers.** The time is held until 10 minutes after the transfer account expires (at most 3 hours). A payment that arrives after the hold has lapsed still books the time if it is free; otherwise it becomes a `payment_without_slot` exception and is refunded automatically.
- Alerts: `payouts_failed` (critical), `payouts_late` (a payout unpaid a day after it was due and not held), `problems_open` (buyer problems holding payouts), plus the refund and no-show alerts.
- Live collection is not authorized by this code alone: live keys, approval ID, legal and commercial approval and owner deployment controls remain launch gates.

## Meeting links

A seller adds a link, or Google Meet makes one. If neither has happened by the link deadline (30 minutes before the start), WantMyTime creates a private video call link (`MEETING_LINK_BASE`, default Jitsi Meet, no account needed to join) and emails it to both people (`AUTO_MEETING_LINKS=false` turns this off). The seller can replace it at any time before the start.
