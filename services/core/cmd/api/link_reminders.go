package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// A seller can change their link once every six months. When the wait is
// over, they get one email saying so.

func linkChangeReadyEmail(name, handle string) emailContent {
	return emailContent{
		Subject:   "You can change your link again",
		Preheader: "Your WantMyTime link is free to change whenever you like.",
		Heading:   "Your link is free to change.",
		Paragraphs: []string{
			"Hi " + firstName(name) + ", it’s been six months since you last changed your link, so you can change it again whenever you like.",
			"Your link is " + linkHost() + "/" + handle + ". If you’re happy with it, there’s nothing to do. If you change it, the old link keeps forwarding to the new one.",
		},
		Action: &emailLink{"Change your link", appOrigin() + "/app/link"},
		Footer: "You’re getting this because you changed your WantMyTime link six months ago.",
	}
}

func linkHost() string {
	return strings.TrimPrefix(strings.TrimPrefix(appOrigin(), "https://"), "http://")
}

func firstName(name string) string {
	if parts := strings.Fields(name); len(parts) > 0 {
		return parts[0]
	}
	return "there"
}

// sendLinkChangeReminders emails sellers whose wait has ended, a few at a time.
func (a *API) sendLinkChangeReminders(ctx context.Context) error {
	for i := 0; i < 20; i++ {
		done, err := a.sendOneLinkChangeReminder(ctx)
		if err != nil || done {
			return err
		}
	}
	return nil
}

func (a *API) sendOneLinkChangeReminder(ctx context.Context) (bool, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var sellerID, handle, name, email string
	var changedAt time.Time
	err = tx.QueryRow(ctx, `SELECT sp.id::text,sp.handle,u.display_name,COALESCE(i.normalized_identifier,''),sp.handle_changed_at
		FROM seller_profiles sp JOIN users u ON u.id=sp.user_id
		LEFT JOIN user_identities i ON i.user_id=u.id AND i.type='email' AND i.verified_at IS NOT NULL AND u.status='active' AND sp.publication_state='published'
		WHERE sp.handle_reminder_due<=now() ORDER BY sp.handle_reminder_due LIMIT 1 FOR UPDATE OF sp SKIP LOCKED`).Scan(&sellerID, &handle, &name, &email, &changedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	// Deleted or unpublished accounts get nothing; the reminder is dropped.
	if email != "" {
		key := "link-change-ready:" + sellerID + ":" + strconv.FormatInt(changedAt.Unix(), 10)
		if !a.deliverEmail(ctx, linkChangeReadyEmail(name, handle).message(email, key)) {
			// Try again in an hour rather than burning through the queue during an outage.
			if _, err = tx.Exec(ctx, `UPDATE seller_profiles SET handle_reminder_due=now()+interval '1 hour' WHERE id=$1`, sellerID); err != nil {
				return false, err
			}
			return true, tx.Commit(ctx)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE seller_profiles SET handle_reminder_due=NULL WHERE id=$1`, sellerID); err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}
