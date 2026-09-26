package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

func (a *API) emailConfigured() bool {
	if provider := os.Getenv("EMAIL_PROVIDER"); provider == "resend" || provider == "sendly" {
		return os.Getenv("EMAIL_API_KEY") != "" && os.Getenv("EMAIL_FROM") != ""
	}
	return a.env != "production" && os.Getenv("SMTP_HOST") != ""
}

func enqueueBookingNotifications(ctx context.Context, tx pgx.Tx, bookingID string) error {
	participants, err := store.New(tx).BookingParticipants(ctx, bookingID)
	if err != nil {
		return err
	}
	buyerID, sellerID, starts := participants.BuyerUserID, participants.SellerUserID, participants.StartsAt
	if err := enqueueNotification(ctx, tx, bookingID, buyerID, "booking_confirmed_buyer", bookingID+":booking-confirmed:buyer", time.Now()); err != nil {
		return err
	}
	if err := enqueueNotification(ctx, tx, bookingID, sellerID, "booking_confirmed_seller", bookingID+":booking-confirmed:seller", time.Now()); err != nil {
		return err
	}
	if err := enqueuePush(ctx, tx, bookingID, sellerID, "new_booking_seller", bookingID+":push:new-booking", time.Now()); err != nil {
		return err
	}
	if err := enqueueCalendarSync(ctx, tx, bookingID); err != nil {
		return err
	}
	return enqueueBookingReminders(ctx, tx, bookingID, buyerID, sellerID, starts)
}

func enqueueBookingReminders(ctx context.Context, tx pgx.Tx, bookingID, buyerID, sellerID string, starts time.Time) error {
	now := time.Now()
	var durationMinutes int32
	if err := tx.QueryRow(ctx, `SELECT duration_minutes FROM bookings WHERE id=$1`, bookingID).Scan(&durationMinutes); err != nil {
		return err
	}
	items := []struct {
		kind, audience string
		due            time.Time
	}{}
	for _, reminder := range []struct {
		delta time.Duration
		label string
	}{{24 * time.Hour, "24h"}, {time.Hour, "1h"}} {
		due := starts.Add(-reminder.delta)
		if due.After(now) {
			items = append(items,
				struct {
					kind, audience string
					due            time.Time
				}{"booking_reminder_" + reminder.label + "_buyer", "buyer", due},
				struct {
					kind, audience string
					due            time.Time
				}{"booking_reminder_" + reminder.label + "_seller", "seller", due})
		}
	}
	for _, reminder := range []struct {
		delta time.Duration
		label string
	}{{24 * time.Hour, "24h"}, {2 * time.Hour, "2h"}} {
		due := starts.Add(-reminder.delta)
		if due.After(now) {
			items = append(items, struct {
				kind, audience string
				due            time.Time
			}{"meeting_link_due_" + reminder.label + "_seller", "seller", due})
		}
	}
	// Ask the buyer for a review an hour after the session ends.
	items = append(items, struct {
		kind, audience string
		due            time.Time
	}{"review_request_buyer", "buyer", starts.Add(time.Duration(durationMinutes)*time.Minute + time.Hour)})
	for _, item := range items {
		recipient := buyerID
		if item.audience == "seller" {
			recipient = sellerID
		}
		key := bookingID + ":" + item.kind + ":" + starts.UTC().Format(time.RFC3339)
		if err := enqueueNotification(ctx, tx, bookingID, recipient, item.kind, key, item.due); err != nil {
			return err
		}
	}
	return nil
}

func enqueueNotification(ctx context.Context, tx pgx.Tx, bookingID, recipientID, kind, eventKey string, due time.Time) error {
	return store.New(tx).EnqueueNotification(ctx, store.EnqueueNotificationParams{EventKey: eventKey, BookingID: bookingID, RecipientUserID: recipientID, Kind: kind, DueAt: due})
}

// enqueueBookingEvent queues an immediate email about a booking. referenceID
// names the reschedule or cancellation request the email is about.
func enqueueBookingEvent(ctx context.Context, tx pgx.Tx, bookingID, recipientID, kind, eventKey string, referenceID *string) error {
	return store.New(tx).EnqueueNotification(ctx, store.EnqueueNotificationParams{EventKey: eventKey, BookingID: bookingID, RecipientUserID: recipientID, Kind: kind, DueAt: time.Now(), ReferenceID: referenceID})
}

func enqueueOfferEvent(ctx context.Context, tx pgx.Tx, offerID, recipientID, kind, eventKey string) error {
	return store.New(tx).EnqueueOfferNotification(ctx, store.EnqueueOfferNotificationParams{EventKey: eventKey, OfferID: offerID, RecipientUserID: recipientID, Kind: kind})
}

func (a *API) enqueueMeetingReady(ctx context.Context, tx pgx.Tx, bookingID string) error {
	return store.New(tx).EnqueueMeetingReady(ctx, bookingID)
}

func (a *API) enqueueRescheduleEmails(ctx context.Context, tx pgx.Tx, bookingID, requestID string) error {
	return store.New(tx).EnqueueRescheduleEmails(ctx, store.EnqueueRescheduleEmailsParams{RequestID: requestID, BookingID: bookingID})
}

func enqueueCancellationReview(ctx context.Context, tx pgx.Tx, cancellationID string) error {
	return store.New(tx).EnqueueCancellationReview(ctx, cancellationID)
}

func (a *API) runNotificationWorker(ctx context.Context) {
	for {
		err := a.processNotificationBatch(ctx)
		if reminderErr := a.sendLinkChangeReminders(ctx); err == nil {
			err = reminderErr
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			a.log().ErrorContext(ctx, "notification worker batch failed", "error", err.Error())
		}
		a.beat(ctx, "notifications", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (a *API) processNotificationBatch(ctx context.Context) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	if err = q.FailExhaustedNotifications(ctx); err != nil {
		return err
	}
	jobs, err := q.ClaimDueNotifications(ctx, 10)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	for _, jobID := range jobs {
		a.deliverNotification(ctx, jobID)
	}
	return nil
}

func (a *API) deliverNotification(ctx context.Context, jobID string) {
	target, err := store.New(a.db).NotificationTarget(ctx, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return // claimed by another worker after a stale claim; it owns the job now
	}
	if err != nil {
		a.finishNotification(ctx, jobID, false, "DATABASE_ERROR")
		return
	}
	var content emailContent
	var recipient, code string
	if target.IsOffer {
		content, recipient, code = a.offerEmail(ctx, jobID)
	} else if target.IsPayment {
		content, recipient, code = a.exceptionEmail(ctx, jobID)
	} else {
		content, recipient, code = a.bookingEmail(ctx, jobID)
	}
	switch code {
	case "":
	case "STALE":
		a.cancelNotification(ctx, jobID)
		return
	default:
		a.finishNotification(ctx, jobID, false, code)
		return
	}
	// The job ID is stable across retries, so the provider drops a duplicate
	// if an earlier attempt was accepted but its response was lost.
	if a.deliverEmail(ctx, content.message(recipient, "aside-notification-"+jobID)) {
		a.finishNotification(ctx, jobID, true, "")
	} else {
		a.finishNotification(ctx, jobID, false, "DELIVERY_FAILED")
	}
}

func appOrigin() string {
	return strings.TrimRight(envOr("PUBLIC_APP_ORIGIN", "https://wantmytime.com"), "/")
}

// bookingEmail builds the email for a booking notification. A non-empty code
// means "do not send": STALE cancels the job, anything else is a failure.
func (a *API) bookingEmail(ctx context.Context, jobID string) (emailContent, string, string) {
	n, err := store.New(a.db).NotificationContext(ctx, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return emailContent{}, "", "RECIPIENT_UNAVAILABLE"
	}
	if err != nil {
		return emailContent{}, "", "DATABASE_ERROR"
	}
	kind := n.Kind
	sellerZone := zoneOr(n.Timezone, time.UTC)
	own := sellerZone
	other, otherName := zoneOr(n.BuyerTimezone, sellerZone), n.BuyerName
	if !n.RecipientIsSeller {
		own = zoneOr(n.RecipientTimezone, sellerZone)
		other, otherName = sellerZone, n.SellerName
	}
	otherLabel := otherName + "’s time"
	starts, duration := n.StartsAt, int(n.DurationMinutes)
	ends := starts.Add(time.Duration(duration) * time.Minute)
	bookingURL := appOrigin() + "/booking/" + n.BookingID
	with := n.SellerName + " (@" + n.Handle + ")"
	if n.RecipientIsSeller {
		with = n.BuyerName
	}
	timeFacts := func(label string, at time.Time) []emailFact {
		facts := []emailFact{{label, formatWhen(at, own)}}
		if sameOffset(at, own, other) {
			return facts
		}
		return append(facts, emailFact{otherLabel, formatClock(at, other)})
	}
	baseFacts := append(timeFacts("When", starts), emailFact{"Length", fmt.Sprintf("%d minutes", duration)}, emailFact{"With", with})
	calendar := &calendarEvent{UID: n.BookingID, Sequence: int(n.CalendarSequence), Start: starts, End: ends, Summary: fmt.Sprintf("%d minutes with %s", duration, otherName), Description: "Booked on WantMyTime. The private meeting link is on your booking page: " + bookingURL, URL: bookingURL}
	upcoming := n.BookingState == "confirmed" && starts.After(time.Now())
	c := emailContent{Action: &emailLink{"View booking", bookingURL}}
	short := formatShort(starts, own)

	switch kind {
	case "booking_confirmed_buyer", "booking_confirmed_seller":
		simulated := n.PaymentState == "simulated"
		if !simulated && (n.PaymentState != "paid" || !n.HasAllocation) {
			return c, "", "VERIFIED_ALLOCATION_REQUIRED"
		}
		c.Facts = baseFacts
		c.Calendar = calendar
		if kind == "booking_confirmed_buyer" {
			c.Subject = fmt.Sprintf("Confirmed: %d minutes with %s, %s", duration, n.SellerName, short)
			c.Heading = "You're booked with " + n.SellerName + "."
			c.Paragraphs = []string{"Your time is confirmed. " + n.SellerName + " will add a private meeting link before the call, and we'll email it to you as soon as it's ready."}
			if !simulated {
				var fee int64
				_ = a.db.QueryRow(ctx, `SELECT COALESCE(max(buyer_fee_minor),0) FROM payment_attempts WHERE booking_id=$1 AND canonical_state='success'`, n.BookingID).Scan(&fee)
				paid := formatMoney(n.Currency, n.GrossMinor+fee)
				if fee > 0 {
					paid += " (includes " + formatMoney(n.Currency, fee) + " payment fee)"
				}
				c.Facts = append(c.Facts, emailFact{"Paid", paid})
				c.Links = append(c.Links, emailLink{"Download your receipt", bookingURL + "/receipt"})
			}
			c.Facts = append(c.Facts, emailFact{"Cancellation", cancellationRule.Summary})
			if !simulated {
				c.Notes = append(c.Notes, "If something goes wrong, report it from your booking page by "+formatWhen(ends.Add(disputeWindow()), own)+".")
			}
		} else {
			c.Subject = fmt.Sprintf("New booking: %s, %s", n.BuyerName, short)
			c.Heading = n.BuyerName + " booked time with you."
			c.Paragraphs = []string{"Add a private meeting link at least 30 minutes before the start so " + n.BuyerName + " can join."}
			c.Action = &emailLink{"Add the meeting link", bookingURL}
			if !simulated {
				c.Facts = append(c.Facts, emailFact{"Paid", formatMoney(n.Currency, n.GrossMinor)}, emailFact{"Your share", formatMoney(n.Currency, n.SellerEntitlementMinor)})
				c.Notes = append(c.Notes, "Your share is paid to your payout account about "+humanDuration(payoutDelay())+" after the session ends, once the buyer's time to report a problem has passed. Payout status is shown in your WantMyTime account.")
			}
		}
		if simulated {
			c.Notes = append(c.Notes, "This is a local test booking. No money was collected.")
		}
		c.Preheader = formatWhen(starts, own)
	case "meeting_link_ready_buyer":
		if len(n.MeetingUrl) == 0 {
			return c, "", "MEETING_LINK_NOT_READY"
		}
		if !upcoming && !(n.BookingState == "confirmed" && starts.After(time.Now().Add(-15*time.Minute))) {
			return c, "", "STALE"
		}
		meeting, decryptErr := a.decryptMeetingLink(n.MeetingUrl)
		if decryptErr != nil {
			return c, "", "MEETING_LINK_UNAVAILABLE"
		}
		c.Subject = "Your meeting link for " + short
		c.Heading = "Your meeting link is ready."
		c.Paragraphs = []string{n.SellerName + " added the link for your call. Use it at the scheduled time."}
		c.Facts = append(baseFacts, emailFact{"Meeting link", string(meeting)})
		c.Action = &emailLink{"Join the call", string(meeting)}
		c.Links = []emailLink{{"Open your booking page", bookingURL}}
		c.Notes = []string{"Keep this link to yourself. Your booking page always has the latest details."}
	case "booking_reminder_24h_buyer", "booking_reminder_1h_buyer", "booking_reminder_24h_seller", "booking_reminder_1h_seller":
		if !upcoming {
			return c, "", "STALE"
		}
		soon := "Tomorrow"
		if strings.Contains(kind, "_1h_") {
			soon = "In 1 hour"
		}
		c.Facts = baseFacts
		if strings.HasSuffix(kind, "_buyer") {
			c.Subject = fmt.Sprintf("%s: your time with %s", soon, n.SellerName)
			c.Heading = fmt.Sprintf("%s: your time with %s.", soon, n.SellerName)
			if len(n.MeetingUrl) > 0 {
				if meeting, decryptErr := a.decryptMeetingLink(n.MeetingUrl); decryptErr == nil {
					c.Facts = append(c.Facts, emailFact{"Meeting link", string(meeting)})
					c.Action = &emailLink{"Join the call", string(meeting)}
				}
			} else {
				c.Paragraphs = []string{"The meeting link isn't in yet. We'll email it as soon as " + n.SellerName + " adds it."}
			}
		} else {
			c.Subject = fmt.Sprintf("%s: %s at %s", soon, n.BuyerName, formatClockOnly(starts, own))
			c.Heading = fmt.Sprintf("%s: your time with %s.", soon, n.BuyerName)
			if len(n.MeetingUrl) == 0 {
				c.Paragraphs = []string{"You haven't added a meeting link yet. Add it at least 30 minutes before the start."}
				c.Action = &emailLink{"Add the meeting link", bookingURL}
			}
		}
	case "meeting_link_due_24h_seller", "meeting_link_due_2h_seller":
		if !upcoming || len(n.MeetingUrl) > 0 {
			return c, "", "STALE"
		}
		deadline := starts.Add(-30 * time.Minute)
		c.Subject = "Add the meeting link for " + n.BuyerName + "'s booking"
		c.Heading = "Your booking still needs a meeting link."
		c.Paragraphs = []string{"Add a private meeting link before " + formatWhen(deadline, own) + " so " + n.BuyerName + " can join."}
		if autoMeetingLinksEnabled() {
			c.Paragraphs = append(c.Paragraphs, "If you don’t, we’ll create a video call link at that time and send it to you both.")
		}
		c.Facts = baseFacts
		c.Action = &emailLink{"Add the meeting link", bookingURL}
	case "meeting_link_auto_seller":
		if !upcoming && !(n.BookingState == "confirmed" && starts.After(time.Now().Add(-15*time.Minute))) {
			return c, "", "STALE"
		}
		if len(n.MeetingUrl) == 0 {
			return c, "", "MEETING_LINK_NOT_READY"
		}
		meeting, decryptErr := a.decryptMeetingLink(n.MeetingUrl)
		if decryptErr != nil {
			return c, "", "MEETING_LINK_UNAVAILABLE"
		}
		c.Subject = "We made a call link for " + n.BuyerName + "'s booking"
		c.Heading = "Your call has a link now."
		c.Paragraphs = []string{"No meeting link had been added, so we created one and sent it to " + n.BuyerName + " too. Join from it at the start time."}
		c.Facts = append(baseFacts, emailFact{"Meeting link", string(meeting)})
		c.Action = &emailLink{"Join the call", string(meeting)}
		c.Notes = []string{"Prefer your own link? Add it on the booking page; it replaces this one and " + n.BuyerName + " is told."}
	case "reschedule_requested_buyer", "reschedule_requested_seller":
		if n.RescheduleStartsAt == nil || n.RescheduleState != "pending" || !upcoming {
			return c, "", "STALE"
		}
		c.Subject = otherName + " asked to move your booking"
		c.Heading = otherName + " asked to move your booking."
		c.Paragraphs = []string{"Nothing changes unless you accept. If you decline or don't respond, the booking stays at its current time."}
		c.Facts = append(timeFacts("Current time", starts), timeFacts("Proposed time", *n.RescheduleStartsAt)...)
		if n.RescheduleExpiresAt != nil {
			c.Facts = append(c.Facts, emailFact{"Respond by", formatWhen(*n.RescheduleExpiresAt, own)})
		}
		c.Action = &emailLink{"Review the request", bookingURL}
	case "reschedule_declined_buyer", "reschedule_declined_seller":
		c.Subject = otherName + " kept the original time"
		c.Heading = otherName + " declined the new time."
		c.Paragraphs = []string{"Your booking stays as it was."}
		c.Facts = baseFacts
	case "reschedule_accepted_buyer", "reschedule_accepted_seller":
		c.Subject = "New time confirmed: " + short
		c.Heading = "Your booking has a new time."
		c.Paragraphs = []string{"The length and price are unchanged. The calendar file attached replaces the earlier one."}
		c.Facts = baseFacts
		c.Calendar = calendar
	case "cancellation_requested_buyer", "cancellation_requested_seller":
		if !upcoming {
			return c, "", "STALE"
		}
		c.Subject = otherName + " asked to cancel your booking"
		c.Heading = otherName + " asked to cancel your booking."
		if n.RecipientIsSeller {
			c.Paragraphs = []string{"It’s your call. If you agree, cancel the booking from its page and they get a full refund. If not, you don’t need to do anything: the booking stays and you’re paid as usual."}
			c.Action = &emailLink{"Review the request", bookingURL + "#cancel"}
		} else {
			c.Paragraphs = []string{"The booking stays as it is unless one of you cancels it from the booking page."}
		}
		c.Facts = baseFacts
	case "cancellation_reviewed_buyer", "cancellation_reviewed_seller":
		c.Subject = "Update on the cancellation request"
		c.Heading = "The cancellation request has been reviewed."
		c.Paragraphs = []string{"The booking and any payment remain unchanged. Open the booking for details, or contact support if you need help."}
		c.Facts = baseFacts
	case "booking_cancelled_buyer", "booking_cancelled_seller":
		if n.BookingState != "cancelled" {
			return c, "", "STALE"
		}
		byRecipient := (n.CancelledByRole == "seller") == n.RecipientIsSeller
		if byRecipient {
			c.Subject = "You cancelled your booking with " + otherName
			c.Heading = "Your booking is cancelled."
		} else {
			c.Subject = otherName + " cancelled your booking"
			c.Heading = otherName + " cancelled your booking."
		}
		c.Paragraphs = []string{"The time is free again and has been removed from calendars that use the attached file."}
		c.Facts = baseFacts
		c.Action = &emailLink{"View booking", bookingURL}
		refundDone := n.RefundState == "processed" || n.RefundState == "not_required"
		switch {
		case n.PaymentState == "simulated":
			c.Notes = append(c.Notes, "This was a local test booking. No money was collected.")
		case !n.RecipientIsSeller && n.RefundMinor > 0:
			c.Facts = append(c.Facts, emailFact{"Your refund", formatMoney(n.Currency, n.RefundMinor)})
			if !refundDone {
				c.Notes = append(c.Notes, "We'll email you when the refund has been sent. Banks can take up to 10 working days to show it.")
			}
		case !n.RecipientIsSeller:
			c.Facts = append(c.Facts, emailFact{"Refund", "None: it was less than 24 hours before the start"})
		case n.RefundSellerShareMinor > 0 && n.RefundSellerLiability == "receivable":
			c.Facts = append(c.Facts, emailFact{"Refunded to the buyer", formatMoney(n.Currency, n.RefundMinor)})
			c.Notes = append(c.Notes, "Your share of the payment ("+formatMoney(n.Currency, n.RefundSellerShareMinor)+") was already paid to you, so it will be taken from your next payouts, at most half of each one until it is repaid.")
		case n.RefundSellerShareMinor > 0:
			c.Facts = append(c.Facts, emailFact{"Refunded to the buyer", formatMoney(n.Currency, n.RefundMinor)})
			if kept := n.SellerEntitlementMinor - n.RefundSellerShareMinor; kept > 0 {
				c.Notes = append(c.Notes, "You keep "+formatMoney(n.Currency, kept)+" under the cancellation rule. It is paid out with your other payouts.")
			} else {
				c.Notes = append(c.Notes, "There is no payout for this booking.")
			}
		}
		c.Calendar = calendar
		c.Calendar.Method = "CANCEL"
		c.Calendar.Sequence++
	case "refund_processed_buyer":
		if n.RefundState != "processed" {
			return c, "", "STALE"
		}
		c.Subject = "Your refund of " + formatMoney(n.Currency, n.RefundMinor) + " is on its way"
		c.Heading = "Your refund has been sent."
		c.Paragraphs = []string{"We've sent " + formatMoney(n.Currency, n.RefundMinor) + " back to the account you paid from. Banks can take up to 10 working days to show it."}
		c.Facts = []emailFact{{"Booking", fmt.Sprintf("%d minutes with %s", duration, n.SellerName)}, {"Refund", formatMoney(n.Currency, n.RefundMinor)}}
	case "no_show_reported_buyer", "no_show_reported_seller":
		if n.NoShowState != "open" || n.NoShowResolvesAt == nil {
			return c, "", "STALE"
		}
		c.Subject = otherName + " says you didn't join your booking"
		c.Heading = otherName + " reported that you didn't join."
		consequence := "If it stands, the booking is recorded as missed and no refund is due."
		if n.RecipientIsSeller {
			consequence = "If it stands, the buyer gets a full refund and there is no payout for this booking. Your payout is on hold until then."
		}
		c.Paragraphs = []string{"If you were there, tell us from the booking page before the deadline and WantMyTime will review both sides. " + consequence}
		c.Facts = append(baseFacts, emailFact{"Respond by", formatWhen(*n.NoShowResolvesAt, own)})
		c.Action = &emailLink{"Respond", bookingURL}
	case "no_show_resolved_buyer", "no_show_resolved_seller":
		switch {
		case n.NoShowState == "rejected":
			c.Subject = "Update on the no-show report"
			c.Heading = "The no-show report was not upheld."
			c.Paragraphs = []string{"After reviewing both sides, WantMyTime found the booking went ahead. Nothing changes."}
		case n.NoShowState == "accepted" && n.NoShowAbsentRole == "seller":
			c.Subject = "The booking was recorded as missed by the seller"
			c.Heading = "The booking was recorded as missed."
			if n.RecipientIsSeller {
				c.Paragraphs = []string{"The buyer receives a full refund, so there is no payout for this booking."}
			} else {
				c.Paragraphs = []string{"You'll receive a full refund. We'll email you when it has been sent."}
			}
		case n.NoShowState == "accepted":
			c.Subject = "The booking was recorded as missed by the buyer"
			c.Heading = "The booking was recorded as missed."
			c.Paragraphs = []string{"The buyer didn't join, so the booking stands and no refund is due."}
		default:
			return c, "", "STALE"
		}
		c.Facts = baseFacts
	case "review_request_buyer":
		if (n.BookingState != "confirmed" && n.BookingState != "completed") || n.HasReview {
			return c, "", "STALE"
		}
		c.Subject = "How was your time with " + n.SellerName + "?"
		c.Heading = "How was your time with " + n.SellerName + "?"
		c.Paragraphs = []string{"A quick rating helps other people choose, and helps " + n.SellerName + " too. It takes a few seconds."}
		c.Action = &emailLink{"Leave a review", bookingURL + "#review"}
	case "review_received_seller":
		if n.ReviewRating == 0 {
			return c, "", "STALE"
		}
		c.Subject = fmt.Sprintf("New review: %s from %s", strings.Repeat("★", int(n.ReviewRating))+strings.Repeat("☆", 5-int(n.ReviewRating)), reviewerName(n.BuyerName))
		c.Heading = reviewerName(n.BuyerName) + " left you a review."
		if n.ReviewBody != "" {
			c.Paragraphs = []string{"“" + n.ReviewBody + "”"}
		}
		c.Facts = []emailFact{{"Rating", fmt.Sprintf("%d out of 5", n.ReviewRating)}}
		c.Action = &emailLink{"Reply publicly", bookingURL + "#review"}
		c.Notes = []string{"You can reply once. Your reply appears under the review on your page."}
	case "payout_sent_seller":
		if n.PayoutState != "paid" {
			return c, "", "STALE"
		}
		booking := fmt.Sprintf("%d minutes with %s, %s", duration, n.BuyerName, short)
		c.Action = &emailLink{"View payouts", appOrigin() + "/app/money/payouts"}
		if n.PayoutNetMinor > 0 {
			c.Subject = formatMoney(n.Currency, n.PayoutNetMinor) + " is on its way to you"
			c.Heading = "Your payout has been sent."
			c.Paragraphs = []string{"We've sent your earnings for this booking to your payout account. Most banks and wallets show it within minutes; some take until the next working day."}
			c.Facts = []emailFact{{"Booking", booking}, {"Amount", formatMoney(n.Currency, n.PayoutNetMinor)}, {"Paid to", n.PayoutBankName + " ••" + n.PayoutAccountLast4}}
		} else {
			c.Subject = "Your earnings repaid an earlier refund"
			c.Heading = "Your earnings for this booking repaid an earlier refund."
			c.Paragraphs = []string{"Nothing was sent this time, because this booking's earnings covered money refunded to a buyer after you had been paid."}
			c.Facts = []emailFact{{"Booking", booking}}
		}
		if n.PayoutRecoveryMinor > 0 {
			c.Facts = append(c.Facts, emailFact{"Kept to repay an earlier refund", formatMoney(n.Currency, n.PayoutRecoveryMinor)})
		}
	case "problem_reported_seller":
		c.Subject = n.BuyerName + " reported a problem with a booking"
		c.Heading = n.BuyerName + " reported a problem."
		c.Paragraphs = []string{
			"Open the booking to read what they said and answer within " + humanDuration(problemResponseWindow()) + ": refund them in full, refund part of the price, or tell us you disagree.",
			"If you don't answer in time, the buyer is refunded the price in full automatically. Your payout for this booking waits until it's settled.",
		}
		c.Facts = baseFacts
	case "problem_disputed_buyer":
		c.Subject = "The seller disagrees with the problem you reported"
		c.Heading = "WantMyTime will decide."
		c.Paragraphs = []string{"The seller has told us they see it differently. We'll look at both sides and email you both the outcome. The seller isn't paid for this booking until then."}
		c.Facts = baseFacts
	case "payout_failed_seller":
		if n.PayoutState != "failed" {
			return c, "", "STALE"
		}
		amount := n.PayoutNetMinor
		if amount <= 0 {
			amount = n.SellerEntitlementMinor
		}
		c.Subject = "We couldn't send your payout"
		c.Heading = "Your payout didn't go through."
		c.Paragraphs = []string{
			"Your bank or wallet provider didn't accept the payment for this booking. We'll try again automatically over the next few days.",
			"If your account details have changed or might be wrong, update them under Money and we'll send it again as soon as the new account is ready.",
		}
		c.Facts = append(baseFacts, emailFact{"Amount", formatMoney(n.Currency, amount)})
		if n.PayoutBankName != "" {
			c.Facts = append(c.Facts, emailFact{"Account", n.PayoutBankName + " ••" + n.PayoutAccountLast4})
		}
		c.Action = &emailLink{"Check your payout account", appOrigin() + "/app/money/payouts"}
	case "problem_resolved_buyer", "problem_resolved_seller":
		if n.IssueResolution == "" {
			return c, "", "STALE"
		}
		c.Subject = "Update on the reported problem"
		c.Heading = "The reported problem is settled."
		c.Paragraphs = []string{n.IssueResolution}
		c.Facts = baseFacts
		switch {
		case n.RefundMinor > 0 && !n.RecipientIsSeller:
			c.Facts = append(c.Facts, emailFact{"Your refund", formatMoney(n.Currency, n.RefundMinor)})
			c.Notes = append(c.Notes, "We'll email you when the refund has been sent.")
		case n.RefundMinor > 0:
			c.Facts = append(c.Facts, emailFact{"Refunded to the buyer", formatMoney(n.Currency, n.RefundMinor)})
			if n.RefundSellerLiability == "payable" && n.RefundSellerShareMinor < n.SellerEntitlementMinor {
				c.Notes = append(c.Notes, "The rest of your payout will be sent shortly.")
			}
		case n.RecipientIsSeller && n.PaymentState == "paid":
			c.Notes = append(c.Notes, "No refund was due. Your payout will be sent shortly.")
		}
	default:
		return c, "", "UNKNOWN_NOTIFICATION"
	}
	return c, n.RecipientEmail, ""
}

// offerEmail builds the email for an offer notification.
func (a *API) offerEmail(ctx context.Context, jobID string) (emailContent, string, string) {
	n, err := store.New(a.db).OfferNotificationContext(ctx, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return emailContent{}, "", "RECIPIENT_UNAVAILABLE"
	}
	if err != nil {
		return emailContent{}, "", "DATABASE_ERROR"
	}
	own := zoneOr(n.SellerTimezone, time.UTC)
	if !n.RecipientIsSeller {
		own = zoneOr(n.RecipientTimezone, own)
	}
	currency := "NGN"
	_ = a.db.QueryRow(ctx, `SELECT sp.currency::text FROM offers o JOIN seller_profiles sp ON sp.id=o.seller_id WHERE o.id=$1`, n.OfferID).Scan(&currency)
	amount := formatMoney(currency, n.AmountMinor)
	length := fmt.Sprintf("%d minutes", n.DurationMinutes)
	sellerURL := appOrigin() + "/app/offers/" + n.OfferID
	buyerURL := appOrigin() + "/offer/" + n.OfferID
	c := emailContent{}
	requireState := func(states ...string) bool {
		for _, s := range states {
			if n.OfferState == s {
				return true
			}
		}
		return false
	}
	switch n.Kind {
	case "offer_received_seller":
		if !requireState("pending") {
			return c, "", "STALE"
		}
		c.Subject = fmt.Sprintf("New offer from %s: %s for %d minutes", n.BuyerName, amount, n.DurationMinutes)
		c.Heading = n.BuyerName + " made you an offer."
		c.Paragraphs = []string{"You can accept it, decline it, or send one counteroffer."}
		c.Facts = []emailFact{{"Offer", amount}, {"Length", length}, {"Respond by", formatWhen(n.ExpiresAt, own)}}
		c.Action = &emailLink{"Review the offer", sellerURL}
	case "offer_countered_buyer":
		if !requireState("countered") {
			return c, "", "STALE"
		}
		c.Subject = fmt.Sprintf("%s countered your offer: %s", n.SellerName, amount)
		c.Heading = n.SellerName + " sent a counteroffer."
		c.Paragraphs = []string{"Accept it to choose a time and pay, or decline it. No money has been taken."}
		c.Facts = []emailFact{{"Their price", amount}, {"Length", length}, {"Respond by", formatWhen(n.ExpiresAt, own)}}
		c.Action = &emailLink{"Review the counteroffer", buyerURL}
	case "offer_accepted_buyer":
		if !requireState("agreed") {
			return c, "", "STALE"
		}
		c.Subject = n.SellerName + " accepted your offer"
		c.Heading = n.SellerName + " accepted your offer."
		c.Paragraphs = []string{"Choose a time and pay to confirm the booking. Your agreed price is held until the deadline below."}
		c.Facts = []emailFact{{"Agreed price", amount}, {"Length", length}}
		if n.CheckoutExpiresAt != nil {
			c.Facts = append(c.Facts, emailFact{"Book by", formatWhen(*n.CheckoutExpiresAt, own)})
		}
		c.Action = &emailLink{"Choose a time", buyerURL}
	case "offer_accepted_seller":
		if !requireState("agreed", "converted") {
			return c, "", "STALE"
		}
		c.Subject = n.BuyerName + " accepted your counteroffer"
		c.Heading = n.BuyerName + " accepted your counteroffer."
		c.Paragraphs = []string{"They now choose a time and pay. You'll get a booking email as soon as the payment is confirmed."}
		c.Facts = []emailFact{{"Agreed price", amount}, {"Length", length}}
		c.Action = &emailLink{"View the offer", sellerURL}
	case "offer_declined_buyer":
		c.Subject = n.SellerName + " declined your offer"
		c.Heading = n.SellerName + " declined your offer."
		c.Paragraphs = []string{"No money was taken. You can make a new offer from their page."}
		c.Facts = []emailFact{{"Your offer", amount}, {"Length", length}}
		c.Action = &emailLink{"Visit their page", appOrigin() + "/" + n.Handle}
	case "offer_declined_seller":
		c.Subject = n.BuyerName + " declined your counteroffer"
		c.Heading = n.BuyerName + " declined your counteroffer."
		c.Paragraphs = []string{"The offer is closed. Nothing else is needed from you."}
		c.Facts = []emailFact{{"Your counteroffer", amount}, {"Length", length}}
	case "offer_withdrawn_seller":
		c.Subject = n.BuyerName + " withdrew their offer"
		c.Heading = n.BuyerName + " withdrew their offer."
		c.Paragraphs = []string{"The offer is closed. Nothing else is needed from you."}
	case "offer_withdrawn_buyer":
		c.Subject = n.SellerName + " withdrew their counteroffer"
		c.Heading = n.SellerName + " withdrew their counteroffer."
		c.Paragraphs = []string{"No money was taken. You can make a new offer from their page."}
		c.Action = &emailLink{"Visit their page", appOrigin() + "/" + n.Handle}
	default:
		return c, "", "UNKNOWN_NOTIFICATION"
	}
	return c, n.RecipientEmail, ""
}

func (a *API) finishNotification(ctx context.Context, jobID string, sent bool, code string) {
	q := store.New(a.db)
	if sent {
		_ = q.MarkNotificationSent(ctx, jobID)
		return
	}
	_ = q.MarkNotificationFailed(ctx, store.MarkNotificationFailedParams{ErrorCode: code, ID: jobID})
}

func (a *API) cancelNotification(ctx context.Context, jobID string) {
	_ = store.New(a.db).MarkNotificationCancelled(ctx, jobID)
}

// --- time and money formatting for emails -----------------------------------

func zoneOr(name string, fallback *time.Location) *time.Location {
	if loc, err := loadNamedTimezone(name); err == nil {
		return loc
	}
	return fallback
}

// zoneAbbrev returns a short zone name such as WAT or BST, or GMT+4 when the
// zone database has no abbreviation.
func zoneAbbrev(t time.Time, loc *time.Location) string {
	name, offset := t.In(loc).Zone()
	if strings.IndexFunc(name, func(r rune) bool { return r >= 'A' && r <= 'Z' }) >= 0 {
		return name
	}
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	hours, minutes := offset/3600, (offset%3600)/60
	if minutes != 0 {
		return fmt.Sprintf("GMT%s%d:%02d", sign, hours, minutes)
	}
	return fmt.Sprintf("GMT%s%d", sign, hours)
}

// cityName turns an IANA zone into a place name ("America/New_York" -> "New York").
func cityName(loc *time.Location) string {
	name := loc.String()
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return strings.ReplaceAll(name, "_", " ")
}

func formatWhen(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("Monday, 2 January 2006 at 3:04 PM") + " " + zoneAbbrev(t, loc)
}

func formatShort(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("Mon 2 Jan, 3:04 PM") + " " + zoneAbbrev(t, loc)
}

func formatClockOnly(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("3:04 PM") + " " + zoneAbbrev(t, loc)
}

func formatClock(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("Mon 2 Jan, 3:04 PM") + " " + zoneAbbrev(t, loc) + " (" + cityName(loc) + ")"
}

// sameOffset reports whether two zones show the same wall-clock time at t.
func sameOffset(t time.Time, a, b *time.Location) bool {
	_, x := t.In(a).Zone()
	_, y := t.In(b).Zone()
	return x == y
}

// currencySymbols are the symbols people expect for the currencies sellers
// are paid in; anything else is written with its ISO code.
var currencySymbols = map[string]string{"NGN": "₦", "GHS": "GH₵", "KES": "KSh ", "ZAR": "R", "USD": "$", "GBP": "£", "EUR": "€"}

// formatMoney renders minor units with thousands separators: NGN 1050000 -> ₦10,500.
func formatMoney(currency string, minor int64) string {
	currency = strings.TrimSpace(currency)
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	whole := fmt.Sprintf("%d", minor/100)
	var grouped strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(r)
	}
	amount := grouped.String()
	if fraction := minor % 100; fraction != 0 {
		amount += fmt.Sprintf(".%02d", fraction)
	}
	if symbol, ok := currencySymbols[currency]; ok {
		return sign + symbol + amount
	}
	return sign + currency + " " + amount
}
