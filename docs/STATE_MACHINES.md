# State machines

This file summarizes code currently present in the Go API and PostgreSQL migrations. Provider sandbox and production behavior still need their own verification.

## Profile and readiness

- A user verifies an email identity, claims one available handle, and edits a persistent seller profile and versioned price.
- A profile may be published while collection readiness is false. Pausing a profile stops new bookings; prior private bookings remain accessible to their participants.
- Payment readiness is a provider/owner gate and cannot be asserted by a seller-facing profile update.

## Booking and payment

- A buyer selects an available slot and creates an expiring quote/hold. The server enforces price, duration, schedule, buffers, timezone rules, overlap exclusion and buyer scope.
- Local simulation, when explicitly enabled in local environment, creates a clearly simulated booking and no allocation or settlement item.
- Provider checkout is a separate, gated flow. Browser return is not proof of payment; server verification or a signed webhook is required. A verified event atomically records booking, immutable fee terms, one settlement route, allocation, ledger journal and notification jobs.
- Duplicate, late, mismatched, reversed or otherwise exceptional payment events remain explicit exceptions. They do not silently create a normal paid booking.
- Booking and money status are distinct: confirmed does not prove a provider payment; payment received does not prove seller bank settlement.

## Offers

- A verified buyer creates a pending offer. The seller can agree, decline, or issue one counteroffer; the buyer can accept that counter, decline, or withdraw while allowed by the current state.
- Mutations require the authorized participant and current version. Expiry is enforced. Agreement freezes the negotiated amount and does not charge the buyer.
- Agreed offer checkout uses the same quote, hold, payment verification, allocation and once-only conversion path as fixed-price booking.

## Conversation lifecycle

- A confirmed booking reserves the agreed slot. Participants can access it privately, add/view its booking-specific encrypted HTTPS meeting link, download an ICS event, report an issue, or independently record completion.
- Meeting-link reminders and booking email jobs are persisted in the notification outbox and retried when an email transport is configured.
- A reschedule proposal leaves the original slot active. The other participant's acceptance revalidates availability and atomically moves the reservation, retaining price and duration and writing history.
- Cancelling: either participant can cancel an upcoming confirmed booking straight away (`confirmed → cancelled`).
  - The seller's cancellation policy (flexible, moderate or strict) is frozen onto the booking when it is made, and decides the buyer's refund. A buyer who cancels within an hour of booking, for a time at least a day away, gets a full refund.
  - A seller cancellation always refunds in full.
  - The confirm step must match the refund amount the person was shown, so a refund that drops in between is never a surprise.
  - Cancelling releases the time, cancels pending emails and reschedule requests, removes the calendar event, and emails both people with a calendar cancellation.
  - A buyer outside the refund window can ask the seller for a full refund instead (`booking_cancellation_requests`). The seller is emailed and sees the request on the booking; saying yes is cancelling (full refund). The request never holds the payout and closes itself (`resolved`) when the booking time arrives or the booking is cancelled. Operations can still see and close it.
- Refunds: `pending_approval | queued → submitted → processed`, or `failed`. Simulated bookings record `not_required`. Each refund records whether the seller share comes out of the still-held payout (`payable`) or is owed back after it was paid (`receivable`, a seller recovery). Processing writes the ledger, and sets the booking's `payment_state` to `refunded` or `partially_refunded`.
- Problems: the buyer can report a problem from booking until the dispute window closes (`DISPUTE_WINDOW_MINUTES`, default 2 hours after the end); the seller can report one any time. A buyer's open problem holds the payout until operations resolve it, optionally with a refund.
- No-shows: from 10 minutes after the start until the dispute window closes, a participant can report the other absent.
  - The absent person can dispute it for 24 hours. A report that the seller was absent holds the payout meanwhile. Undisputed, it stands: `confirmed → no_show_seller` (full refund) or `no_show_buyer` (no refund).
  - Operations decide disputed reports: seller absent, buyer absent, or both attended. The last rejects the report and the booking stays confirmed.
- Reviews: after the session ends, and for 60 days, the buyer can leave one rating (1–5) with an optional note.
  - Reviews are only possible on confirmed or completed bookings, not cancelled or missed ones.
  - The seller can reply once. Operations can hide or restore a review, with a reason and an audit record.
  - A review request email goes out an hour after the session, unless the buyer has already reviewed.

## Payments that cannot become a booking

- A verified charge whose hold lapsed is first offered its time again (`reclaimLateSlot`: reactivate the hold if the time is in the future, inside the seller's hours and free); if that works, it books normally.
- Otherwise, and for a second charge on a converted quote, a seller who stopped taking bookings, or a closed offer: `payment_exceptions` row (`open` → `awaiting_provider` → `resolved`) plus a refund with `booking_id NULL`, `payment_exception_id` set and `reason='unbooked_payment'` (`queued` or `pending_approval` → `submitted` → `processed`). Emails: `payment_returning_buyer` at once, `payment_returned_buyer` when sent.
- A processor fee above the platform's share does not block the booking; it is flagged (`settlement_mismatch`, `open`) for review.

## Meeting links

- `meeting_source`: `seller` (added by hand), `google_meet` (Google Calendar), or `auto` (created by WantMyTime at the deadline when neither exists). A seller's own link always replaces the others and re-emails the buyer.

## Settlement and operations

- Each provider allocation has one immutable settlement route. New bookings use `approved_transfer`: the seller payout follows `scheduled → processing → paid`, or `cancelled` (nothing left after refunds) or `failed` (refused or returned). A failed payout moves back to `processing` under a new reference automatically (once after two hours, and whenever the seller saves a different payout account), or when an operator retries it. Holds are computed from the booking, not stored.
- Settlement confirmation is separate from allocation and requires provider-supported evidence. A normalized CSV import matches reference, currency and exact seller entitlement; unmatched and mismatched rows remain reviewable. The accepted file format still requires confirmation against an approved provider account export.
- Operations uses MFA freshness, per-route permission checks, and same-transaction audit records for sensitive actions. There is no generic balance edit, arbitrary payout, or mark-paid action; operators can only retry a failed payout to the seller's own verified account.

## Remaining lifecycle work

- Provider sandbox validation, refund execution, dispute response/evidence submission, and production behavior remain unverified or gated. These actions require provider and owner approval.
