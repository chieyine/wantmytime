package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

// Money flow: the buyer pays WantMyTime in full. The seller's share is held until
// the session has ended and the buyer's window to raise a problem has closed,
// then transferred to the seller's bank account. A problem, a no-show report
// or an unfinished refund holds the payout until it is settled. (A buyer's
// request to cancel outside the policy goes to the seller and closes by
// itself when the booking starts, so it never holds a payout.)

// payoutHoldSQL names why a booking's payout cannot go yet, or ”. It expects
// the booking as b.
const payoutHoldSQL = `CASE
  WHEN b.issue_reason IS NOT NULL AND b.issue_resolved_at IS NULL AND COALESCE(b.issue_reported_by,'buyer')='buyer' THEN 'problem_reported'
  WHEN EXISTS (SELECT 1 FROM no_show_reports ns WHERE ns.booking_id=b.id AND ns.absent_role='seller' AND ns.state IN ('open','disputed')) THEN 'no_show_reported'
  WHEN EXISTS (SELECT 1 FROM refunds rf WHERE rf.booking_id=b.id AND rf.state='failed' AND rf.seller_liability='payable') THEN 'refund_unfinished'
  ELSE '' END`

// payoutReleaseAtSQL is when the payout is due, for booking b and payout po.
// $1 is the delay after the end, in minutes.
// It is never before the buyer's money is in the balance (po.funds_available_at).
const payoutReleaseAtSQL = `GREATEST(b.starts_at + (b.duration_minutes + $1::int) * interval '1 minute', po.funds_available_at)`

func payoutReference(bookingID string, generation int) string {
	ref := "wmt-payout-" + strings.ReplaceAll(bookingID, "-", "")
	if generation > 1 {
		ref += fmt.Sprintf("-g%d", generation)
	}
	return ref
}

var payoutHoldText = map[string]string{
	"problem_reported":  "On hold while WantMyTime reviews a problem the buyer reported.",
	"no_show_reported":  "On hold while a no-show report is settled.",
	"refund_unfinished": "On hold until a refund to the buyer is completed.",
}

// scheduleSellerPayout records the payout when a payment is verified.
func scheduleSellerPayout(ctx context.Context, tx pgx.Tx, provider, bookingID, sellerID string, entitlement int64, currency string, fundsAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO seller_payouts(booking_id,seller_id,provider,entitlement_minor,currency,reference,funds_available_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (booking_id) DO NOTHING`,
		bookingID, sellerID, provider, entitlement, strings.TrimSpace(currency), payoutReference(bookingID, 1), fundsAt)
	return err
}

// --- worker ------------------------------------------------------------------------

func (a *API) processPayouts(ctx context.Context) error {
	if !a.providerEnvironmentConfigured() || payoutsPaused() {
		return nil
	}
	provider, err := a.payoutProvider()
	if err != nil {
		return err
	}
	if err = a.autoRetryFailedPayouts(ctx); err != nil {
		a.log().ErrorContext(ctx, "failed payouts could not be requeued", "error", err.Error())
	}
	rows, err := a.db.Query(ctx, `WITH due AS (
	    SELECT po.id FROM seller_payouts po JOIN bookings b ON b.id=po.booking_id
	    WHERE po.state IN ('scheduled','processing') AND po.next_attempt_at<=now()
	      AND (po.state='processing' OR (`+payoutReleaseAtSQL+` <= now() AND (`+payoutHoldSQL+`)=''))
	    ORDER BY po.next_attempt_at, po.id
	    FOR UPDATE OF po SKIP LOCKED
	    LIMIT 10)
	  UPDATE seller_payouts po SET next_attempt_at=now()+interval '5 minutes',attempts=po.attempts+1,updated_at=now()
	  FROM due WHERE po.id=due.id RETURNING po.id::text`, int(payoutDelay()/time.Minute))
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err = a.advancePayout(ctx, provider, id); err != nil && !errors.Is(err, context.Canceled) {
			a.log().ErrorContext(ctx, "payout could not advance", "payout_id", id, "error", err.Error())
		}
	}
	return nil
}

func (a *API) advancePayout(ctx context.Context, provider payoutProvider, id string) error {
	ready, err := a.releasePayout(ctx, id)
	if err != nil || !ready {
		return err
	}
	return a.sendPayout(ctx, provider, id)
}

func (a *API) payoutWaits(ctx context.Context, id, message string, after time.Duration) error {
	_, err := a.db.Exec(ctx, `UPDATE seller_payouts SET last_error=$2,next_attempt_at=now()+make_interval(secs=>$3::int),updated_at=now() WHERE id=$1 AND state IN ('scheduled','processing')`, id, message, int(after.Seconds()))
	return err
}

// releasePayout fixes the amount owed once the payout is due: the seller's
// share less anything refunded to the buyer, less a part of any earlier
// refund the seller still owes. It reports whether a transfer should be sent.
func (a *API) releasePayout(ctx context.Context, id string) (bool, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var state, bookingID, sellerID, currency string
	var entitlement int64
	if err = tx.QueryRow(ctx, `SELECT state,booking_id::text,seller_id::text,entitlement_minor,currency FROM seller_payouts WHERE id=$1 FOR UPDATE`, id).Scan(&state, &bookingID, &sellerID, &entitlement, &currency); err != nil {
		return false, err
	}
	if state != "scheduled" {
		return state == "processing", tx.Commit(ctx)
	}
	// Lock the booking so a problem reported this instant either lands first
	// (and holds the payout) or after the amount is fixed.
	var hold string
	var due bool
	if err = tx.QueryRow(ctx, `SELECT `+payoutHoldSQL+`, `+payoutReleaseAtSQL+` <= now() FROM bookings b JOIN seller_payouts po ON po.booking_id=b.id WHERE b.id=$2 FOR UPDATE OF b`, int(payoutDelay()/time.Minute), bookingID).Scan(&hold, &due); err != nil {
		return false, err
	}
	if hold != "" || !due {
		return false, tx.Commit(ctx)
	}
	var refunded int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(seller_share_minor),0)::bigint FROM refunds WHERE booking_id=$1 AND seller_liability='payable' AND state<>'failed'`, bookingID).Scan(&refunded); err != nil {
		return false, err
	}
	amount := entitlement - refunded
	if amount <= 0 {
		if _, err = tx.Exec(ctx, `UPDATE seller_payouts SET state='cancelled',amount_minor=0,released_at=now(),last_error=NULL,updated_at=now() WHERE id=$1`, id); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `UPDATE settlement_items SET state='nothing_to_pay' WHERE allocation_id=(SELECT id FROM payment_allocations WHERE booking_id=$1)`, bookingID); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	var bankCode, accountName, last4, bank, destination string
	var sealed []byte
	var usableFrom time.Time
	err = tx.QueryRow(ctx, `SELECT bank_code,account_sealed,account_name,account_last4,bank_name,usable_from,destination_type FROM seller_payout_accounts WHERE seller_id=$1`, sellerID).Scan(&bankCode, &sealed, &accountName, &last4, &bank, &usableFrom, &destination)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, a.payoutWaits(ctx, id, "Waiting for the seller to add a bank account.", time.Hour)
	}
	if err != nil {
		return false, err
	}
	if usableFrom.After(time.Now()) {
		if err = tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, a.payoutWaits(ctx, id, "The seller's new bank account is in its 24-hour safety hold.", time.Until(usableFrom)+time.Minute)
	}
	q := store.New(tx)
	recovery, err := takeRecovery(ctx, q, sellerID, amount)
	if err != nil {
		return false, err
	}
	if recovery > 0 {
		lines := []ledgerLine{{AccountCode: "seller_payable", ScopeID: &sellerID, Side: "debit", Amount: recovery}, {AccountCode: "seller_receivable", ScopeID: &sellerID, Side: "credit", Amount: recovery}}
		if _, err = postLedgerJournal(ctx, tx, "payout_recovery", id, strings.TrimSpace(currency), "earlier refund repaid from payout", lines); err != nil {
			return false, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE seller_payouts SET state='processing',amount_minor=$2,recovery_minor=$3,bank_code=$4,account_sealed=$5,account_name=$6,account_last4=$7,bank_name=$8,destination_type=$9,released_at=now(),next_attempt_at=now(),last_error=NULL,updated_at=now() WHERE id=$1`, id, amount, recovery, bankCode, sealed, accountName, last4, bank, destination); err != nil {
		return false, err
	}
	if amount == recovery {
		// Everything went to repaying an earlier refund: nothing to transfer.
		if err = a.markPayoutPaid(ctx, tx, id, payoutTransfer{}); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	return true, tx.Commit(ctx)
}

// takeRecovery repays earlier refunds from a payout, oldest first, never more
// than recoveryMaxBps of it.
func takeRecovery(ctx context.Context, q *store.Queries, sellerID string, amount int64) (int64, error) {
	open, err := q.LockOpenRecoveries(ctx, sellerID)
	if err != nil || len(open) == 0 {
		return 0, err
	}
	var owed int64
	for _, r := range open {
		owed += int64(r.Outstanding)
	}
	limit := (amount/10000)*recoveryMaxBps() + ((amount%10000)*recoveryMaxBps())/10000
	take := min(owed, limit)
	if take <= 0 {
		return 0, nil
	}
	return take, applyRecovery(ctx, q, sellerID, take)
}

// sendPayout sends (or checks on) the transfer for a released payout.
func (a *API) sendPayout(ctx context.Context, provider payoutProvider, id string) error {
	var state, reference, sellerID, bankCode, accountName, email, currency, destination string
	var sealed []byte
	var amount, recovery int64
	var attempts int
	err := a.db.QueryRow(ctx, `SELECT po.state,po.reference,po.seller_id::text,COALESCE(po.bank_code,''),po.account_sealed,COALESCE(po.account_name,''),COALESCE(i.normalized_identifier,''),po.currency,COALESCE(po.amount_minor,0),po.recovery_minor,po.attempts,po.destination_type
		FROM seller_payouts po JOIN seller_profiles sp ON sp.id=po.seller_id
		LEFT JOIN LATERAL (SELECT normalized_identifier FROM user_identities WHERE user_id=sp.user_id AND type='email' AND verified_at IS NOT NULL ORDER BY verified_at DESC LIMIT 1) i ON true
		WHERE po.id=$1`, id).Scan(&state, &reference, &sellerID, &bankCode, &sealed, &accountName, &email, &currency, &amount, &recovery, &attempts, &destination)
	if err != nil || state != "processing" {
		return err
	}
	// Always look first: a transfer sent before a crash or a timeout must be
	// adopted, never sent again.
	transfer, found, err := provider.findTransfer(ctx, reference)
	if err != nil {
		return a.payoutWaits(ctx, id, "Could not reach the payout provider; retrying.", retryBackoff(attempts))
	}
	if !found {
		accountNumber, openErr := a.openPayoutAccount(sellerID, sealed)
		if openErr != nil || bankCode == "" {
			return a.failPayout(ctx, id, "The payout bank details could not be read. Check the encryption key, then retry.")
		}
		transfer, err = provider.transfer(ctx, payoutTransferRequest{DestinationType: destination, BankCode: bankCode, AccountNumber: accountNumber, AccountName: accountName, Email: email, Reference: reference, Reason: "WantMyTime booking payout", Currency: strings.TrimSpace(currency), AmountMinor: amount - recovery})
		switch {
		case errors.Is(err, errPayoutFundsPending):
			return a.payoutWaits(ctx, id, "Waiting for the buyer's payment to settle into the payout balance.", 15*time.Minute)
		case errors.Is(err, errPayoutRejected):
			return a.failPayout(ctx, id, err.Error())
		case err != nil:
			return a.payoutWaits(ctx, id, "The payout provider did not answer; retrying.", retryBackoff(attempts))
		}
		// The lookup is the authority on status and fee.
		if checked, ok, checkErr := provider.findTransfer(ctx, reference); checkErr == nil && ok {
			transfer = checked
		}
	}
	return a.applyTransferStatus(ctx, id, transfer)
}

func retryBackoff(attempts int) time.Duration {
	return time.Duration(60<<min(max(attempts-1, 0), 5)) * time.Second
}

func (a *API) failPayout(ctx context.Context, id, message string) error {
	tag, err := a.db.Exec(ctx, `UPDATE seller_payouts SET state='failed',last_error=$2,updated_at=now() WHERE id=$1 AND state='processing'`, id, message)
	if err != nil || tag.RowsAffected() != 1 {
		return err
	}
	_, err = a.db.Exec(ctx, payoutFailedEmailSQL, id)
	return err
}

// payoutFailedEmailSQL tells the seller a payout failed (once per attempt):
// it is retried by itself, and at once if they change their payout account.
const payoutFailedEmailSQL = `INSERT INTO notification_outbox(id,event_key,booking_id,recipient_user_id,kind,due_at)
	SELECT gen_random_uuid(),po.id::text||':failed:'||po.generation,po.booking_id,sp.user_id,'payout_failed_seller',now()
	FROM seller_payouts po JOIN seller_profiles sp ON sp.id=po.seller_id WHERE po.id=$1
	ON CONFLICT (event_key) DO NOTHING`

func (a *API) applyTransferStatus(ctx context.Context, id string, t payoutTransfer) error {
	switch t.Status {
	case "success":
		tx, err := a.db.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var state string
		if err = tx.QueryRow(ctx, `SELECT state FROM seller_payouts WHERE id=$1 FOR UPDATE`, id).Scan(&state); err != nil {
			return err
		}
		if state != "processing" {
			return tx.Commit(ctx)
		}
		if err = a.markPayoutPaid(ctx, tx, id, t); err != nil {
			return err
		}
		return tx.Commit(ctx)
	case "failed", "reversed", "otp":
		return a.failPayout(ctx, id, t.Message)
	default:
		return a.payoutWaits(ctx, id, "", 10*time.Minute)
	}
}

// markPayoutPaid records a completed payout inside the caller's transaction.
func (a *API) markPayoutPaid(ctx context.Context, tx pgx.Tx, id string, t payoutTransfer) error {
	var bookingID, sellerID, sellerUser, reference, currency string
	var amount, recovery int64
	err := tx.QueryRow(ctx, `UPDATE seller_payouts po SET state='paid',paid_at=now(),provider_transfer_code=COALESCE(NULLIF($2,''),po.provider_transfer_code),fee_minor=$3,last_error=NULL,updated_at=now()
		FROM seller_profiles sp WHERE po.id=$1 AND po.state='processing' AND sp.id=po.seller_id
		RETURNING po.booking_id::text,po.seller_id::text,sp.user_id::text,po.reference,po.currency,po.amount_minor,po.recovery_minor`, id, t.Reference, max(t.FeeMinor, 0)).Scan(&bookingID, &sellerID, &sellerUser, &reference, &currency, &amount, &recovery)
	if err != nil {
		return err
	}
	currency = strings.TrimSpace(currency)
	net := amount - recovery
	lines := []ledgerLine{}
	if net > 0 {
		lines = append(lines, ledgerLine{AccountCode: "seller_payable", ScopeID: &sellerID, Side: "debit", Amount: net}, ledgerLine{AccountCode: "provider_receivable", Side: "credit", Amount: net})
	}
	if t.FeeMinor > 0 {
		lines = append(lines, ledgerLine{AccountCode: "processor_fee_expense", Side: "debit", Amount: t.FeeMinor}, ledgerLine{AccountCode: "provider_receivable", Side: "credit", Amount: t.FeeMinor})
	}
	if len(lines) > 0 {
		// Each attempt has its own reference, so a retried payout after a
		// reversal gets its own journal.
		if _, err = postLedgerJournal(ctx, tx, "payout", id+":"+reference, currency, "seller payout", lines); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE settlement_items SET state='paid_out' WHERE allocation_id=(SELECT id FROM payment_allocations WHERE booking_id=$1)`, bookingID); err != nil {
		return err
	}
	return enqueueBookingEvent(ctx, tx, bookingID, sellerUser, "payout_sent_seller", id+":paid:"+reference, &id)
}

// checkPayoutReversal handles a transfer the bank sent back after it was
// recorded as paid: the money is back in the balance and owed to the seller
// again, so the payout is marked failed for an operator to retry.
func (a *API) checkPayoutReversal(ctx context.Context, reference string) error {
	var id, sellerID, currency string
	var amount, recovery int64
	err := a.db.QueryRow(ctx, `SELECT id::text,seller_id::text,currency,COALESCE(amount_minor,0),recovery_minor FROM seller_payouts WHERE reference=$1 AND state='paid'`, reference).Scan(&id, &sellerID, &currency, &amount, &recovery)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	provider, err := a.payoutProvider()
	if err != nil {
		return err
	}
	t, found, err := provider.findTransfer(ctx, reference)
	if err != nil || !found || (t.Status != "reversed" && t.Status != "failed") {
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE seller_payouts SET state='failed',last_error=$2,updated_at=now() WHERE id=$1 AND state='paid'`, id, "The bank returned this transfer. Check the seller's bank account, then retry.")
	if err != nil || tag.RowsAffected() != 1 {
		return err
	}
	if _, err = tx.Exec(ctx, payoutFailedEmailSQL, id); err != nil {
		return err
	}
	if net := amount - recovery; net > 0 {
		lines := []ledgerLine{{AccountCode: "provider_receivable", Side: "debit", Amount: net}, {AccountCode: "seller_payable", ScopeID: &sellerID, Side: "credit", Amount: net}}
		if _, err = postLedgerJournal(ctx, tx, "payout_reversal", id+":"+reference, strings.TrimSpace(currency), "seller payout returned by bank", lines); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// autoRetryFailedPayouts sends a failed payout again without anyone having
// to step in: straight away once the seller has saved a different payout
// account (and its safety hold has passed), and on its own 2, 12, 24 and 48
// hours after successive failures, since banks are sometimes unavailable.
// The seller is emailed after each failure. Only a payout still failing
// after all of that raises the payouts_failed alert.
func (a *API) autoRetryFailedPayouts(ctx context.Context) error {
	rows, err := a.db.Query(ctx, `SELECT po.id::text,po.booking_id::text,po.generation,(acct.updated_at>po.updated_at) FROM seller_payouts po
		JOIN seller_payout_accounts acct ON acct.seller_id=po.seller_id AND acct.usable_from<=now()
		WHERE po.state='failed' AND ((acct.updated_at>po.updated_at AND po.generation<20) OR (po.generation<=4 AND po.updated_at<now()-(CASE po.generation WHEN 1 THEN interval '2 hours' WHEN 2 THEN interval '12 hours' WHEN 3 THEN interval '24 hours' ELSE interval '48 hours' END)))
		ORDER BY po.updated_at LIMIT 20`)
	if err != nil {
		return err
	}
	type candidate struct {
		id, bookingID  string
		generation     int
		accountChanged bool
	}
	var list []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.bookingID, &c.generation, &c.accountChanged); err != nil {
			rows.Close()
			return err
		}
		list = append(list, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, c := range list {
		reason := "Retried automatically on the retry schedule (2, 12, 24 and 48 hours after each failure)."
		if c.accountChanged {
			reason = "Retried automatically after the seller updated their payout account."
		}
		tx, txErr := a.db.Begin(ctx)
		if txErr != nil {
			return txErr
		}
		tag, updateErr := tx.Exec(ctx, `UPDATE seller_payouts po SET state='processing',generation=$2,reference=$3,bank_code=a.bank_code,account_sealed=a.account_sealed,account_name=a.account_name,account_last4=a.account_last4,bank_name=a.bank_name,destination_type=a.destination_type,attempts=0,next_attempt_at=now(),last_error=NULL,provider_transfer_code=NULL,updated_at=now()
			FROM seller_payout_accounts a WHERE po.id=$1 AND po.state='failed' AND po.generation=$4 AND a.seller_id=po.seller_id`, c.id, c.generation+1, payoutReference(c.bookingID, c.generation+1), c.generation)
		if updateErr == nil && tag.RowsAffected() == 1 {
			_, updateErr = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),NULL,'payout.auto_retry',$1,$2,'{}')`, c.id, reason)
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

// --- seller: bank account ------------------------------------------------------------

func digitsOnly(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		if r == ' ' || r == '-' {
			return -1
		}
		return 'x'
	}, strings.TrimSpace(s))
}

type payoutAccountInput struct {
	Country string `json:"country"`
	// Type is bank_account (default) or mobile_money.
	Type          string `json:"type"`
	BankCode      string `json:"bank_code"`
	AccountNumber string `json:"account_number"`
	// AccountName is typed by the seller where the bank cannot confirm it
	// (outside Nigeria, and for mobile money wallets).
	AccountName string `json:"account_name"`
}

// valid normalizes the input and checks it against the market's rules.
func (in *payoutAccountInput) valid(m market) bool {
	in.Country = m.Country
	in.Type = strings.TrimSpace(in.Type)
	if in.Type == "" {
		in.Type = "bank_account"
	}
	in.AccountNumber = digitsOnly(in.AccountNumber)
	in.BankCode = strings.TrimSpace(in.BankCode)
	in.AccountName = strings.Join(strings.Fields(in.AccountName), " ")
	n := len(in.AccountNumber)
	if strings.Contains(in.AccountNumber, "x") {
		return false
	}
	switch in.Type {
	case "mobile_money":
		if _, ok := m.operatorName(in.BankCode); !ok {
			return false
		}
		// Wallet numbers are sent in international form: 0712345678 in Kenya
		// becomes 254712345678.
		if strings.HasPrefix(in.AccountNumber, "0") {
			in.AccountNumber = m.PhonePrefix + strings.TrimPrefix(in.AccountNumber, "0")
		} else if !strings.HasPrefix(in.AccountNumber, m.PhonePrefix) {
			in.AccountNumber = m.PhonePrefix + in.AccountNumber
		}
		n = len(in.AccountNumber)
		return n >= 10 && n <= 15
	case "bank_account":
		if !m.BankPayouts || (m.AccountDigits > 0 && n != m.AccountDigits) || (m.AccountDigits == 0 && (n < 6 || n > 20)) || len(in.BankCode) < 2 || len(in.BankCode) > 20 {
			return false
		}
		for _, r := range in.BankCode {
			if !unicode.IsDigit(r) && !unicode.IsLetter(r) && r != '-' && r != '_' {
				return false
			}
		}
		return true
	}
	return false
}

// sellerMarket is the market of the signed-in seller.
func (a *API) sellerMarket(ctx context.Context, userID string) (string, market, error) {
	var sellerID, country string
	if err := a.db.QueryRow(ctx, `SELECT id::text,country::text FROM seller_profiles WHERE user_id=$1`, userID).Scan(&sellerID, &country); err != nil {
		return "", market{}, err
	}
	m, ok := marketForCurrencyCountry(country)
	if !ok {
		return sellerID, market{}, errors.New("country not supported")
	}
	return sellerID, m, nil
}

// marketForCurrencyCountry finds a market by country whether or not it is
// still switched on, so an existing seller can keep being paid.
func marketForCurrencyCountry(country string) (market, bool) {
	for _, m := range allMarkets {
		if m.Country == strings.ToUpper(strings.TrimSpace(country)) {
			return m, true
		}
	}
	return market{}, false
}

func (a *API) listPayoutBanks(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	country := strings.ToUpper(r.URL.Query().Get("country"))
	var m market
	if country == "" {
		_, seller, err := a.sellerMarket(r.Context(), u.ID)
		if err != nil {
			seller = enabledMarkets()[0]
		}
		m = seller
	} else if found, known := marketForCurrencyCountry(country); known {
		m = found
	} else {
		problem(w, 422, "COUNTRY_NOT_SUPPORTED", "Payouts are not available in that country yet.")
		return
	}
	banks := []payoutBank{}
	if m.BankPayouts {
		list, err := a.payoutBanks(r.Context(), m.Country)
		if err != nil && len(m.MobileMoney) == 0 {
			problem(w, 503, "BANKS_UNAVAILABLE", "The bank list could not be loaded. Try again shortly.")
			return
		}
		if err == nil {
			banks = list
		}
	}
	jsonOut(w, 200, map[string]any{"country": m.Country, "currency": m.Currency, "banks": banks, "mobile_money": m.MobileMoney, "name_check": m.NameCheck, "account_digits": m.AccountDigits, "phone_prefix": m.PhonePrefix})
}

func (a *API) bankName(ctx context.Context, country, code string) (string, bool) {
	banks, err := a.payoutBanks(ctx, country)
	if err != nil {
		return "", false
	}
	for _, b := range banks {
		if b.Code == code {
			return b.Name, true
		}
	}
	return "", false
}

func invalidAccountMessage(m market) string {
	if m.AccountDigits > 0 {
		return fmt.Sprintf("Choose your bank and enter your %d-digit account number.", m.AccountDigits)
	}
	return "Choose your bank or mobile money network and enter the account or wallet number."
}

// resolvePayoutAccount shows the seller the name on the account before they
// save it, so a mistyped number is caught.
func (a *API) resolvePayoutAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	_, m, err := a.sellerMarket(r.Context(), u.ID)
	if err != nil {
		problem(w, 409, "SELLER_REQUIRED", "Claim your link before adding a payout account.")
		return
	}
	var in payoutAccountInput
	if decode(r, &in) != nil || !in.valid(m) {
		problem(w, 422, "INVALID_ACCOUNT", invalidAccountMessage(m))
		return
	}
	if in.Type != "bank_account" || !m.NameCheck {
		problem(w, 409, "NAME_CHECK_UNAVAILABLE", "Type the name on the account; it can’t be checked automatically here.")
		return
	}
	if !a.allowPayoutLookup(r.Context(), u.ID) {
		problem(w, 429, "TOO_MANY_LOOKUPS", "Too many account checks. Try again in an hour.")
		return
	}
	provider, err := a.payoutProvider()
	if err != nil {
		problem(w, 503, "PAYOUTS_UNAVAILABLE", "Bank accounts can't be checked right now.")
		return
	}
	name, err := provider.resolveAccount(r.Context(), in.Country, in.BankCode, in.AccountNumber)
	if errors.Is(err, errPayoutRejected) {
		problem(w, 422, "ACCOUNT_NOT_FOUND", "That account number wasn't found at this bank. Check both and try again.")
		return
	}
	if err != nil {
		problem(w, 503, "PAYOUTS_UNAVAILABLE", "The bank didn't answer. Try again shortly.")
		return
	}
	jsonOut(w, 200, map[string]string{"account_name": name})
}

// allowPayoutLookup limits account-name lookups (20 an hour per person), so
// the endpoint can't be used to look up strangers' names.
func (a *API) allowPayoutLookup(ctx context.Context, userID string) bool {
	var n int64
	err := a.db.QueryRow(ctx, `INSERT INTO audit_events(id,actor_id,action,safe_summary) SELECT gen_random_uuid(),$1,'payout_account.lookup','{}' WHERE (SELECT count(*) FROM audit_events WHERE actor_id=$1 AND action='payout_account.lookup' AND created_at>now()-interval '1 hour')<20 RETURNING 1`, userID).Scan(&n)
	return err == nil
}

// accountFingerprint identifies an account without storing its number in
// the clear, keyed so it can't be reversed by guessing numbers.
func (a *API) accountFingerprint(country, bankCode, number string) (string, error) {
	key, err := payoutAccountKey(a)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(country + "|" + bankCode + "|" + number))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (a *API) setPayoutAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	sellerID, m, err := a.sellerMarket(r.Context(), u.ID)
	if err != nil {
		problem(w, 409, "SELLER_REQUIRED", "Claim your link before adding a payout account.")
		return
	}
	var in payoutAccountInput
	if decode(r, &in) != nil || !in.valid(m) {
		problem(w, 422, "INVALID_ACCOUNT", invalidAccountMessage(m))
		return
	}
	var bank, name string
	switch {
	case in.Type == "mobile_money":
		bank, _ = m.operatorName(in.BankCode)
	default:
		known := false
		if bank, known = a.bankName(r.Context(), in.Country, in.BankCode); !known {
			problem(w, 422, "INVALID_BANK", "Choose your bank from the list.")
			return
		}
	}
	if in.Type == "bank_account" && m.NameCheck {
		provider, providerErr := a.payoutProvider()
		if providerErr != nil {
			problem(w, 503, "PAYOUTS_UNAVAILABLE", "Bank accounts can't be saved right now.")
			return
		}
		name, err = provider.resolveAccount(r.Context(), in.Country, in.BankCode, in.AccountNumber)
		if errors.Is(err, errPayoutRejected) {
			problem(w, 422, "ACCOUNT_NOT_FOUND", "That account number wasn't found at this bank. Check both and try again.")
			return
		}
		if err != nil {
			problem(w, 503, "PAYOUTS_UNAVAILABLE", "The bank didn't answer. Try again shortly.")
			return
		}
	} else {
		name = in.AccountName
		if len(name) < 2 || !validName(name) {
			problem(w, 422, "ACCOUNT_NAME_REQUIRED", "Enter the name on the account exactly as your bank or network shows it.")
			return
		}
	}
	sealed, err := a.sealPayoutAccount(sellerID, in.AccountNumber)
	fingerprint, fpErr := a.accountFingerprint(in.Country, in.Type+":"+in.BankCode, in.AccountNumber)
	if err != nil || fpErr != nil {
		problem(w, 503, "PAYOUTS_UNAVAILABLE", "The account could not be stored securely.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The payout account could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	var replaced bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM seller_payout_accounts WHERE seller_id=$1 AND account_fingerprint<>$2)`, sellerID, fingerprint).Scan(&replaced); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The payout account could not be saved.")
		return
	}
	usable := time.Now()
	if replaced {
		usable = usable.Add(payoutAccountCooldown)
	}
	last4 := in.AccountNumber[len(in.AccountNumber)-4:]
	if _, err = tx.Exec(r.Context(), `INSERT INTO seller_payout_accounts(seller_id,provider,country,bank_code,bank_name,account_last4,account_name,account_sealed,account_fingerprint,currency,usable_from,destination_type) VALUES($1,'kora',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (seller_id) DO UPDATE SET provider=EXCLUDED.provider,country=EXCLUDED.country,bank_code=EXCLUDED.bank_code,bank_name=EXCLUDED.bank_name,account_last4=EXCLUDED.account_last4,account_name=EXCLUDED.account_name,
		  account_sealed=EXCLUDED.account_sealed,currency=EXCLUDED.currency,destination_type=EXCLUDED.destination_type,verified_at=now(),
		  usable_from=CASE WHEN seller_payout_accounts.account_fingerprint=EXCLUDED.account_fingerprint THEN seller_payout_accounts.usable_from ELSE EXCLUDED.usable_from END,
		  account_fingerprint=EXCLUDED.account_fingerprint,updated_at=now()`,
		sellerID, in.Country, in.BankCode, bank, last4, name, sealed, fingerprint, m.Currency, usable, in.Type); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The payout account could not be saved.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'payout_account.set',$1,'Seller saved a payout account',jsonb_build_object('bank',$2::text,'last4',$3::text,'replaced',$4::boolean,'country',$5::text,'type',$6::text,'name_checked',$7::boolean))`, u.ID, bank, last4, replaced, in.Country, in.Type, in.Type == "bank_account" && m.NameCheck); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The payout account could not be saved.")
		return
	}
	// A saved payout account is enough to open bookings: money is held until
	// after each call, so buyers stay protected. A seller an operator has put
	// on hold ('held') stays closed.
	if tag, readyErr := tx.Exec(r.Context(), `UPDATE seller_profiles SET readiness_state='ready',public_version=public_version+1 WHERE id=$1 AND readiness_state='incomplete'`, sellerID); readyErr != nil {
		problem(w, 503, "DATABASE_ERROR", "The payout account could not be saved.")
		return
	} else if tag.RowsAffected() == 1 {
		if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'seller.opened_for_bookings',$1,'Payout account saved','{}'::jsonb)`, u.ID); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The payout account could not be saved.")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The payout account could not be saved.")
		return
	}
	a.getPayoutAccount(w, r)
}

func (a *API) getPayoutAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var bank, last4, name, kind, country, currency string
	var usable, verified time.Time
	err := a.db.QueryRow(r.Context(), `SELECT a.bank_name,a.account_last4,a.account_name,a.usable_from,a.verified_at,a.destination_type,a.country::text,a.currency::text FROM seller_payout_accounts a JOIN seller_profiles sp ON sp.id=a.seller_id WHERE sp.user_id=$1`, u.ID).Scan(&bank, &last4, &name, &usable, &verified, &kind, &country, &currency)
	out := map[string]any{"account": nil, "dispute_window_minutes": int(disputeWindow() / time.Minute), "payout_delay_minutes": int(payoutDelay() / time.Minute)}
	if _, m, marketErr := a.sellerMarket(r.Context(), u.ID); marketErr == nil {
		out["market"] = m
	}
	switch {
	case err == nil:
		out["account"] = map[string]any{"bank_name": bank, "account_last4": last4, "account_name": name, "verified_at": verified, "usable_from": usable, "in_safety_hold": usable.After(time.Now()), "type": kind, "country": country, "currency": currency}
	case !errors.Is(err, pgx.ErrNoRows):
		problem(w, 503, "DATABASE_ERROR", "Your payout account could not be loaded.")
		return
	}
	jsonOut(w, 200, out)
}

// --- payout lists -------------------------------------------------------------------

type payoutRow struct {
	ID, BookingID, Seller, BuyerName, State, Hold, Currency, LastError, BankName, Last4, Reference string
	Entitlement, Recovery, Fee                                                                     int64
	Amount                                                                                         *int64
	ReleaseAt                                                                                      time.Time
	PaidAt                                                                                         *time.Time
	Attempts                                                                                       int
}

func (p payoutRow) json(forOps bool) map[string]any {
	out := map[string]any{"id": p.ID, "booking_id": p.BookingID, "buyer_name": p.BuyerName, "state": p.State, "currency": strings.TrimSpace(p.Currency),
		"entitlement_minor": p.Entitlement, "amount_minor": p.Amount, "recovery_minor": p.Recovery, "release_at": p.ReleaseAt, "paid_at": p.PaidAt,
		"bank_name": p.BankName, "account_last4": p.Last4, "hold": p.Hold, "hold_text": payoutHoldText[p.Hold]}
	if p.Amount != nil {
		out["transfer_minor"] = *p.Amount - p.Recovery
	}
	if p.State == "scheduled" || p.State == "processing" || p.State == "failed" {
		out["note"] = p.LastError
	}
	if forOps {
		out["seller"], out["last_error"], out["attempts"], out["reference"], out["fee_minor"] = p.Seller, p.LastError, p.Attempts, p.Reference, p.Fee
	}
	return out
}

func (a *API) queryPayouts(ctx context.Context, where string, args ...any) ([]payoutRow, error) {
	args = append([]any{int(payoutDelay() / time.Minute)}, args...)
	rows, err := a.db.Query(ctx, `SELECT po.id::text,po.booking_id::text,sp.handle,b.buyer_name,po.state,CASE WHEN po.state='scheduled' THEN `+payoutHoldSQL+` ELSE '' END,po.currency,COALESCE(po.last_error,''),
		COALESCE(po.bank_name,''),COALESCE(po.account_last4,''),po.reference,po.entitlement_minor,po.recovery_minor,po.fee_minor,po.amount_minor,`+payoutReleaseAtSQL+`,po.paid_at,po.attempts
		FROM seller_payouts po JOIN bookings b ON b.id=po.booking_id JOIN seller_profiles sp ON sp.id=po.seller_id WHERE `+where+` ORDER BY `+payoutReleaseAtSQL+` DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []payoutRow
	for rows.Next() {
		var p payoutRow
		if err = rows.Scan(&p.ID, &p.BookingID, &p.Seller, &p.BuyerName, &p.State, &p.Hold, &p.Currency, &p.LastError, &p.BankName, &p.Last4, &p.Reference, &p.Entitlement, &p.Recovery, &p.Fee, &p.Amount, &p.ReleaseAt, &p.PaidAt, &p.Attempts); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (a *API) myPayouts(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	rows, err := a.queryPayouts(r.Context(), `sp.user_id=$2`, u.ID)
	if err != nil {
		problem(w, 503, "PAYOUTS_UNAVAILABLE", "Payouts could not be loaded.")
		return
	}
	items := []map[string]any{}
	var upcoming, paid int64
	for _, p := range rows {
		items = append(items, p.json(false))
		switch p.State {
		case "scheduled":
			upcoming += p.Entitlement
		case "processing", "failed":
			upcoming += *p.Amount - p.Recovery
		case "paid":
			paid += *p.Amount - p.Recovery
		}
	}
	currency := "NGN"
	_ = a.db.QueryRow(r.Context(), `SELECT currency::text FROM seller_profiles WHERE user_id=$1`, u.ID).Scan(&currency)
	jsonOut(w, 200, map[string]any{"payouts": items, "upcoming_minor": upcoming, "paid_minor": paid, "paused": payoutsPaused(), "currency": currency})
}

func (a *API) opsPayouts(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	filter := `true`
	if r.URL.Query().Get("state") == "attention" {
		filter = `po.state IN ('failed','scheduled','processing') AND (po.state='failed' OR po.last_error IS NOT NULL)`
	}
	rows, err := a.queryPayouts(r.Context(), filter)
	if err != nil {
		problem(w, 503, "PAYOUTS_UNAVAILABLE", "Payouts could not be loaded.")
		return
	}
	items := []map[string]any{}
	for _, p := range rows {
		items = append(items, p.json(true))
	}
	jsonOut(w, 200, map[string]any{"payouts": items, "paused": payoutsPaused()})
}

// opsRetryPayout sends a failed payout again under a new reference, to the
// seller's current bank account.
func (a *API) opsRetryPayout(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:refund:approve")
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 500 {
		problem(w, 422, "REASON_REQUIRED", "Record what was checked before retrying this payout.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The payout could not be retried.")
		return
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	var bookingID string
	var generation int
	var bankCode, accountName, last4, bank, destination *string
	var sealed []byte
	err = tx.QueryRow(r.Context(), `SELECT po.booking_id::text,po.generation,a.bank_code,a.account_sealed,a.account_name,a.account_last4,a.bank_name,a.destination_type FROM seller_payouts po LEFT JOIN seller_payout_accounts a ON a.seller_id=po.seller_id AND a.usable_from<=now() WHERE po.id=$1 AND po.state='failed' FOR UPDATE OF po`, id).Scan(&bookingID, &generation, &bankCode, &sealed, &accountName, &last4, &bank, &destination)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 409, "PAYOUT_NOT_RETRYABLE", "Only a failed payout can be retried.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The payout could not be retried.")
		return
	}
	if bankCode == nil {
		problem(w, 409, "PAYOUT_ACCOUNT_REQUIRED", "The seller has no usable bank account yet.")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE seller_payouts SET state='processing',generation=$2,reference=$3,bank_code=$4,account_sealed=$5,account_name=$6,account_last4=$7,bank_name=$8,destination_type=$9,attempts=0,next_attempt_at=now(),last_error=NULL,provider_transfer_code=NULL,updated_at=now() WHERE id=$1`,
		id, generation+1, payoutReference(bookingID, generation+1), *bankCode, sealed, *accountName, *last4, *bank, *destination); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The payout could not be retried.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'payout.retry',$2,$3,'{}')`, actor.ID, id, strings.TrimSpace(in.Reason)); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The payout could not be retried.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The payout could not be retried.")
		return
	}
	jsonOut(w, 200, map[string]string{"state": "processing"})
}
