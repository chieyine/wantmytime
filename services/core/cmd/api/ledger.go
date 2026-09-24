package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

type ledgerLine struct {
	AccountCode string
	ScopeID     *string
	Side        string
	Amount      int64
}

// postLedgerJournal is called only inside the same database transaction as a
// verified economic state change. Source identity and canonical digest make
// retries safe; an unbalanced journal is rejected before it is written.
func postLedgerJournal(ctx context.Context, tx pgx.Tx, sourceType, sourceID, currency, description string, lines []ledgerLine) (string, error) {
	if sourceType == "" || sourceID == "" || len(currency) != 3 || len(lines) < 2 {
		return "", errors.New("invalid ledger journal identity")
	}
	var debit, credit int64
	for _, line := range lines {
		if line.AccountCode == "" || line.Amount <= 0 {
			return "", errors.New("invalid ledger entry")
		}
		switch line.Side {
		case "debit":
			if debit > math.MaxInt64-line.Amount {
				return "", errors.New("ledger total overflow")
			}
			debit += line.Amount
		case "credit":
			if credit > math.MaxInt64-line.Amount {
				return "", errors.New("ledger total overflow")
			}
			credit += line.Amount
		default:
			return "", errors.New("invalid ledger side")
		}
	}
	if debit != credit {
		return "", fmt.Errorf("unbalanced journal: debit=%d credit=%d", debit, credit)
	}
	canonical, err := json.Marshal(struct {
		Currency    string
		Description string
		Lines       []ledgerLine
	}{currency, description, lines})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	q := store.New(tx)
	journalID, err := q.InsertLedgerJournal(ctx, store.InsertLedgerJournalParams{SourceType: sourceType, SourceID: sourceID, Currency: currency, Description: description, RequestDigest: digest[:]})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, lookupErr := q.LedgerJournalBySource(ctx, store.LedgerJournalBySourceParams{SourceType: sourceType, SourceID: sourceID})
		if lookupErr != nil {
			return "", lookupErr
		}
		if !equalBytes(existing.RequestDigest, digest[:]) {
			return "", errors.New("ledger source identity reused with different journal")
		}
		return existing.ID, nil
	}
	if err != nil {
		return "", err
	}
	for _, line := range lines {
		accountID, accountErr := q.InsertLedgerAccount(ctx, store.InsertLedgerAccountParams{AccountCode: line.AccountCode, Currency: currency, ScopeID: line.ScopeID})
		if errors.Is(accountErr, pgx.ErrNoRows) {
			accountID, accountErr = q.FindLedgerAccount(ctx, store.FindLedgerAccountParams{AccountCode: line.AccountCode, Currency: currency, ScopeID: line.ScopeID})
		}
		if accountErr != nil {
			return "", accountErr
		}
		if err = q.InsertLedgerEntry(ctx, store.InsertLedgerEntryParams{JournalID: journalID, AccountID: accountID, Side: line.Side, AmountMinor: line.Amount}); err != nil {
			return "", err
		}
	}
	return journalID, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
