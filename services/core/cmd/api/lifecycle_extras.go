package main

import (
	"context"
	"os"
	"strings"
)

// closeStartedCancellationRequests closes buyers' requests to cancel that
// the seller let stand: once the booking time arrives the booking has gone
// ahead (or a no-show report covers it), so the request has nothing left
// to decide.
func (a *API) closeStartedCancellationRequests(ctx context.Context) error {
	_, err := a.db.Exec(ctx, `UPDATE booking_cancellation_requests cr SET state='resolved',resolution='Closed when the booking time arrived; the seller kept the booking.',resolved_at=now()
		FROM bookings b WHERE cr.booking_id=b.id AND cr.state='open' AND (b.starts_at<=now() OR b.state<>'confirmed')`)
	return err
}

// autoMeetingLinksEnabled: when a seller has not added a meeting link by the
// deadline (30 minutes before the start) and no Google Meet link was made,
// WantMyTime creates a video call link so the buyer is never left without
// one. AUTO_MEETING_LINKS=false turns this off.
func autoMeetingLinksEnabled() bool { return os.Getenv("AUTO_MEETING_LINKS") != "false" }

// meetingLinkBase is where created call links live (default Jitsi Meet,
// which needs no account to join).
func meetingLinkBase() string {
	base := strings.TrimSpace(os.Getenv("MEETING_LINK_BASE"))
	if !strings.HasPrefix(base, "https://") {
		base = "https://meet.jit.si/"
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base
}

// createMissingMeetingLinks gives every booking that reached its link
// deadline without a link a fresh private call link, and emails both people.
func (a *API) createMissingMeetingLinks(ctx context.Context) error {
	if !autoMeetingLinksEnabled() {
		return nil
	}
	if _, err := meetingLinkKey(a); err != nil {
		return nil // links cannot be stored without the key; nothing to do
	}
	rows, err := a.db.Query(ctx, `SELECT b.id::text,sp.user_id::text FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE b.state='confirmed' AND b.meeting_url IS NULL AND b.meeting_deadline<=now() AND b.starts_at>now()-interval '15 minutes' ORDER BY b.starts_at LIMIT 20`)
	if err != nil {
		return err
	}
	type due struct{ booking, seller string }
	var list []due
	for rows.Next() {
		var d due
		if err = rows.Scan(&d.booking, &d.seller); err != nil {
			rows.Close()
			return err
		}
		list = append(list, d)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, d := range list {
		link := meetingLinkBase() + "WantMyTime-" + strings.ToUpper(randomHex(10))
		sealed, sealErr := a.encryptMeetingLink([]byte(link))
		if sealErr != nil {
			return sealErr
		}
		tx, txErr := a.db.Begin(ctx)
		if txErr != nil {
			return txErr
		}
		tag, updateErr := tx.Exec(ctx, `UPDATE bookings SET meeting_url=$2,meeting_ready_at=now(),meeting_source='auto' WHERE id=$1 AND meeting_url IS NULL AND state='confirmed'`, d.booking, sealed)
		if updateErr == nil && tag.RowsAffected() == 1 {
			if updateErr = a.enqueueMeetingReady(ctx, tx, d.booking); updateErr == nil {
				updateErr = enqueueBookingEvent(ctx, tx, d.booking, d.seller, "meeting_link_auto_seller", d.booking+":meeting-link-auto", nil)
			}
		}
		if updateErr != nil {
			_ = tx.Rollback(ctx)
			return updateErr
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
