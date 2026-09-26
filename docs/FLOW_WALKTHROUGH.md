# Buyer and seller flows, simulated end to end

Updated: 2026-09-25

Every step a seller and a buyer take, traced through the code: what the person does, what the web page calls, what the API and database do, which emails go out, and what happens when something goes wrong. Each flow was walked through against the source; where a step broke or needed a person to step in, it was fixed in this pass and is marked **Fixed**.

Conventions: amounts are minor units in the seller's currency; "hold" is a `slot_reservations` row of kind `hold`; "guest session" is a booking-only or access-only session (`sessions.guest_scope`).

---

## Part 1: the seller

### S1. Claim a link

1. Home page → type a name → `/claim?handle=…`.
2. `/claim`: link, display name, **country they're paid in** (only shown when more than one country is switched on; guessed from the browser timezone), price for 30 minutes in that currency, email.
3. Submit: `GET /api/v1/handles/{handle}/availability` (taken or held for 180 days after a deletion → "That link is taken"), then `POST /api/v1/auth/challenges {purpose: claim}` → 8-digit code emailed (5 per email per hour).
4. The draft (including country and browser timezone) waits in `sessionStorage`; `/verify` takes the code. Typing the 8th digit submits.
5. `POST /api/v1/auth/challenges/{id}/verify` → account created or found, email marked verified, 30-day session cookie; the browser timezone is saved for emails.
6. `POST /api/v1/me/link` → `seller_profiles` (published, `readiness_state='incomplete'`, `country`, `currency`), first `pricing_versions` row in that currency → `/app` (Home, with the setup checklist).
7. The link is suggested from the seller's name at sign-up. Later, `PUT /api/v1/me/link/handle` changes it, once every 6 months; the seller gets an email when they can change it again. The old link keeps forwarding (`GET /api/v1/people/{old}` answers 308, the page answers 301) and stays reserved for that seller; deleting the account puts every old link on hold.

Edge cases: code expired or wrong 5 times → "invalid or expired", ask again. Handle taken between the check and the save → 409 "already claimed". Country not switched on → 422. **Fixed:** the seller's country and currency are now chosen here (they were hard-wired to Nigeria/NGN).

### S2. Set hours

1. `/app/availability`: first visit is pre-filled with weekdays 9–5 in the seller's timezone (every IANA zone is available). Save → `PUT /api/v1/me/availability` (15-minute steps, no overlaps, notice 0–7 days, horizon 1–365 days, buffer 0–4 hours).
2. Close single dates → `PUT /api/v1/me/availability/overrides/{date}`.

Changing the timezone on Your link also moves the weekly hours to it, so slots never silently disappear.

### S3. Add a payout account (this opens bookings)

1. `/app/money/payouts` loads the seller's country: `GET /api/v1/me/payout-account` and `GET /api/v1/payout-banks`.
2. **Nigeria:** choose a bank, type the 10-digit number → `POST /api/v1/me/payout-account/resolve` shows the account holder's name from the bank (20 checks an hour). **Fixed:** the name check sent Kora the country code (`NG`) where it expects the currency (`NGN`), so it would have failed for everyone.
3. **Ghana and Kenya:** bank account or mobile money ( network + wallet number, stored as `233…`/`254…`); the seller types the name on the account because Kora can't confirm it there.
4. Save → `PUT /api/v1/me/payout-account`: number encrypted (AES-GCM, bound to the seller), only the last four digits readable; `readiness_state` becomes `ready` unless an operator has put the seller on hold. The overview now says "You're open for bookings".
5. Changing to a different account later: payouts to it start 24 hours later (anti-takeover). Any failed payout is sent again to it automatically after that.

### S4. Share

Home (`/app`): copy link and the system share sheet. The public page shows the price in the seller's currency, how buyers pay, the cancellation rule (the same for every seller) and reviews. Link previews (OpenGraph image) show the price in the seller's currency.

### S5. A booking comes in

When a buyer's payment is confirmed (B6):

- Email to seller: "New booking: {buyer}, {time}" with the calendar file, what was paid, their share, and when it's paid out.
- If Google Calendar is connected: a private event on their calendar and, if they want, a Google Meet link, which is saved as the meeting link and emailed to the buyer.
- Reminders to both at 24 hours and 1 hour; reminders to the seller at 24 hours and 2 hours if there's no meeting link yet.

### S6. The meeting link

1. The seller pastes a link on the booking page (`PATCH /api/v1/bookings/{id}/meeting`, HTTPS only) → encrypted, emailed to the buyer.
2. Or Google Meet makes one (S5).
3. **Fixed:** if neither has happened by 30 minutes before the start, WantMyTime now creates a private video call link (Jitsi by default), saves it, and emails it to both people. Before, the buyer could reach the start time with no link at all. The seller can still replace it.

### S7. Reschedule

Either person picks a new free time (`POST /api/v1/bookings/{id}/reschedules`, checked against hours, notice, horizon, buffers and Google busy times). The other gets an email and accepts or declines on the booking page within 48 hours; nothing moves until they accept. On accept: the reservation moves atomically, reminders are re-queued, calendars updated, both emailed with the new calendar file.

### S8. Cancel (seller side)

`GET …/cancellation-preview` then `POST …/cancel` with the refund shown. A seller cancelling always refunds the buyer in full, payment fee included; the time is freed, calendars updated, both emailed, and there is no payout.

### S9. The buyer asks to cancel outside the policy

**Fixed:** the request used to go to an operator and held the seller's payout until an operator acted, even after the call had happened. Now:

- The seller gets an email ("{buyer} asked to cancel … it's your call") and sees the reason on the booking page.
- Saying yes = cancelling (S8): the buyer gets a full refund.
- Saying no = doing nothing. The request never holds the payout and closes itself when the booking time arrives.

### S10. Offers (sellers in "let people make an offer" or "both" mode)

Every link has a price (`fixed`). Under Your page a seller can also let people offer a different price (`both`). With `both`, the public page leads with the price and "Pick a time", and adds "Make an offer" underneath; fixed-price quotes and offers are both accepted.


1. Offer arrives → email "New offer from {buyer}: {amount} for {length}".
2. `/app/offers/{id}`: accept, counter once, or decline (`POST /api/v1/offers/{id}/accept|counter|decline`, with the version to stop stale answers). The buyer is emailed each time.
3. After agreement, the buyer has 24 hours to pick a time and pay; the seller gets the booking email when they do.

**Fixed:** offers can no longer be sent to a seller who has no payout account yet (the buyer could never have paid).

### S11. After the call: getting paid

1. Payout is scheduled at payment time (`seller_payouts`, `scheduled`).
2. Release time = end of session + 3 hours (and, for pay with bank and mobile money, after the money has settled: next working day). The lifecycle worker checks every minute.
3. Held while: the buyer has an open problem report; a report that the seller didn't join is open or disputed; a refund of this booking failed.
4. Released: amount = share − refunds of this booking − (up to half, by default) any earlier refund the seller still owes. Kora transfer to the bank account or wallet, looked up by reference first so it's never sent twice. Email: "{amount} is on its way to you".
5. Failures: insufficient balance (money not settled yet) → waits and retries every 15 minutes, never pre-funded. Refused/failed/returned → `failed`. **Fixed:** failed payouts now retry by themselves once after two hours, and again as soon as the seller saves a different payout account; before, every one needed an operator.

### S12. No-shows, problems, reviews

- The seller can report the buyer didn't join (from 10 minutes after the start until 2 hours after the end). The buyer can dispute within 24 hours; undisputed, it stands (no refund) and the payout goes out.
- The seller can report a problem at any time; it doesn't hold their own payout.
- Reviews: the buyer can review for 60 days; the seller gets an email and can reply once, publicly.

### S13. Money page, receipts, data

- `/app/money`: on the way and paid out, in the seller's currency; each payout's state and any hold reason.
- Receipts show the seller the price, WantMyTime's 5% and their share.
- `/app/settings/data`: download everything (3 a day); delete the account once nothing is in flight (the link is held for 180 days).

---

## Part 2: the buyer

### B1. Open the link

`/{handle}` (server-rendered): name, photo, social link, rating, price in the seller's currency, lengths, how you can pay, cancellation policy, reviews. Paused or not-yet-open sellers show "Bookings open soon". **Fixed:** an offer-mode seller without a payout account used to take offers anyway; now the page says bookings open soon.

### B2. Pick a time

`/book/new?seller=…&duration=…`:

1. Times are shown in the buyer's own timezone (browser), with a switch to the seller's. The page opens on the first day in the next two weeks that has a free time.
2. `GET /api/v1/people/{handle}/slots?date&duration&tz` returns free starts, honouring hours, overrides, notice, horizon, buffers, other holds and bookings, and Google busy times (refreshed if older than 2 minutes).
3. Name and email, then "Continue to payment".

**Fixed:** opening this page for an offer-mode seller now sends the buyer to the offer page instead of failing at the next step.

### B3. Hold the time (no code needed)

1. `POST /api/v1/bookings/start {email}` → booking-only guest session for that email (not treated as verified; 10 per email per hour).
2. `POST /api/v1/quotes` (idempotency key) → re-validates everything, 10-minute hold, quote in the seller's currency. A buyer can hold at most 3 times at once; picking a new time with the same seller releases their older unpaid hold.
3. → `/checkout/{quote}`.

Taken while choosing → "That time was just taken. Choose another time."

### B4. Pay

`/checkout/{quote}` shows who, when, how long and the price, then one button per way of paying **with the total including the payment fee** (`GET /api/v1/quotes/{id}` returns `payment_methods` and `method_fees` for the seller's currency).

- **Bank transfer (Nigeria):** `POST /api/v1/quotes/{id}/checkout {method: bank_transfer}` → a one-off account (bank, number, name, exact amount, expiry). The hold is extended to 10 minutes after the account expires (max 3 hours). The page checks every 5 seconds (`/verify-payment`) and moves to the booking as soon as it's confirmed. Reloading the page reuses the same account.
- **Pay with bank, mobile money:** redirect to Kora's hosted page; back to `/payment/return`, which confirms the payment with Kora. **Fixed:** that page now keeps checking by itself for about two minutes while a payment is still being confirmed (it used to stop and ask the buyer to press a button), explains a declined payment, and links back to the payment page.
- No card payments: buyers pay in the seller's currency by that country's local method.

Short transfer → Kora returns it; the page says to send the exact amount.

### B5. Payment confirmed

By the page's check, the return page, or Kora's signed webhook (verified independently with Kora), whichever comes first; all three are idempotent. One transaction creates the booking, allocation (5% / 95% of the price), payout schedule, ledger journal, emails and calendar job.

- **Late transfer. Fixed:** used to become an exception an operator had to refund. Now, if the time is still free and inside the seller's hours, the booking goes ahead. Only if the time was taken is the money refunded, automatically, with an email.
- **Paid twice** (transfer then pay with bank). **Fixed:** the second payment is refunded automatically and the buyer is emailed; the booking stands.
- **Seller stopped taking bookings, or an offer closed, while paying. Fixed:** refunded automatically with an email.
- **Kora changes its fees.** Kora adds its own current fee to what the buyer pays, so nothing breaks and nobody else pays it: the seller keeps 95% of the price and WantMyTime keeps 5%.

### B6. Booking page and emails

- Email: "Confirmed: {length} with {seller}, {time}" with the calendar file, what was paid (including the payment fee), the cancellation policy, the receipt link and the deadline to report a problem.
- `/booking/{id}` (the same browser works straight away; the session lasts 7 days): time, what they paid, add to calendar, meeting link when ready, reschedule, cancel, report a problem or no-show, review, receipt.
- Meeting link email as soon as it exists (seller, Google Meet, or the automatic link at 30 minutes before).

### B7. Coming back on another device

Email links open `/booking/{id}` → "Open it with your email" → `/access` → code → access session covering that email's bookings, paid-but-unconverted payments and offers → back to the page.

**Fixed:** a buyer who came back this way to an accepted offer couldn't pay for it (the API only accepted the session that had sent the offer). Now any session that can see the offer can check out.

### B8. Change or cancel

- Reschedule as in S7.
- Cancel: the preview shows the refund under the booking's frozen policy (full refund within an hour of booking if the time is at least a day away). The confirmation must match the amount shown, so it can't drop in between. The payment fee isn't refunded on a buyer cancellation. Refunds go out automatically with `REFUNDS_ENABLED=true`; the buyer is emailed when sent.
- Outside the policy: "Ask {seller} for a full refund" (S9).

### B9. After the call

- Problem: report within 2 hours of the end. The seller has 24 hours to refund in full, refund part, or disagree; no answer refunds the buyer in full. Only a disagreement goes to WantMyTime. The seller's payout waits until it's settled.
- Seller didn't join: report from 10 minutes after the start; undisputed for 24 hours → full refund including the payment fee; disputed → WantMyTime decides.
- "It took place" (optional) and a review (1–5, first name only) up to 60 days later; a review request email goes out an hour after the call.
- Receipt: price, payment fee, total paid, any refund, method and reference.

### B10. Offers (buyer side)

1. `/offer/new`: length, amount in the seller's currency, name, email → code (offers need a confirmed email) → offer sent, seller emailed.
2. Counter → email; accept or decline on `/offer/{id}`.
3. Accepted → email "Choose a time"; `/offer/{id}` opens on the first day with free times, in the buyer's timezone. Pick → `POST /api/v1/offers/{id}/checkout` → same checkout as B4.

**Fixed:** the offer page showed times in the buyer's zone under a label naming the seller's zone, and opened on a date with no times; the "email notifications are not configured" warning on the offer form was wrong and is gone.

---

## Part 3: what still needs a person, by design

- Deciding a disputed no-show, or a problem report the seller disagrees with (both hold the payout until decided). Everything undisputed settles itself: a problem report the seller doesn't answer within 24 hours refunds the buyer in full.
- A payout still failing after four automatic retries over about four days (the seller is emailed each time and a new payout account sends it at once).
- Rare leftovers the system can't safely settle alone (a payment whose reference doesn't match, a refund Kora rejected outright). Each raises an alert.

Everything else in both journeys runs by itself.
