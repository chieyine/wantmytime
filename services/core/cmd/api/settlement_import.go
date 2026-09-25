package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

func (a *API) opsSettlementImportRows(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id::text,provider_reference,settlement_reference,amount_minor,currency,settled_at,reconciliation_state,received_at FROM settlement_import_rows WHERE environment=$1 AND reconciliation_state<>'duplicate' AND ($2::timestamptz IS NULL OR (received_at,id)<($2,$3::uuid)) ORDER BY received_at DESC,id DESC LIMIT 100`, environmentOf(a), cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "SETTLEMENTS_UNAVAILABLE", "Unmatched provider rows could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, reference, state, currency string
		var settlementReference *string
		var amount int64
		var settledAt, receivedAt time.Time
		if err = rows.Scan(&id, &reference, &settlementReference, &amount, &currency, &settledAt, &state, &receivedAt); err != nil {
			problem(w, 503, "SETTLEMENTS_UNAVAILABLE", "Unmatched provider rows could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "transaction_reference": reference, "settlement_reference": settlementReference, "amount_minor": amount, "currency": currency, "settled_at": settledAt, "state": state, "received_at": receivedAt})
	}
	if err = rows.Err(); err != nil {
		problem(w, 503, "SETTLEMENTS_UNAVAILABLE", "Unmatched provider rows could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"rows": items, "next_cursor": nextListCursor(items, "received_at", "id")})
}

// importSettlementCSV accepts normalized operator-supplied rows for review.
// Exact matching does not prove that the provider settled the seller.
func (a *API) importSettlementCSV(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:settlement:import")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		problem(w, 413, "IMPORT_TOO_LARGE", "Settlement import exceeds the 1 MB limit.")
		return
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = -1
	headers, err := reader.Read()
	if err != nil {
		problem(w, 422, "INVALID_IMPORT", "Settlement import is not a valid CSV file.")
		return
	}
	index := map[string]int{}
	for i, h := range headers {
		index[strings.TrimSpace(strings.ToLower(h))] = i
	}
	for _, name := range []string{"transaction_reference", "settlement_reference", "amount_minor", "currency", "settled_at"} {
		if _, found := index[name]; !found {
			problem(w, 422, "INVALID_IMPORT", "CSV must include transaction_reference, settlement_reference, amount_minor, currency, and settled_at columns.")
			return
		}
	}
	environment := environmentOf(a)
	importID := ""
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "IMPORT_UNAVAILABLE", "Settlement import could not be started.")
		return
	}
	defer tx.Rollback(r.Context())
	q := store.New(tx)
	if importID, err = randomUUID(); err != nil {
		problem(w, 503, "IMPORT_UNAVAILABLE", "Settlement import could not be started.")
		return
	}
	var matched, unmatched, amountMismatch, currencyMismatch, duplicates int
	for line := 2; ; line++ {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil || len(row) != len(headers) {
			problem(w, 422, "INVALID_IMPORT", "A CSV row has an invalid number of columns.")
			return
		}
		get := func(key string) string { return strings.TrimSpace(row[index[key]]) }
		reference, settlementRef, currency := get("transaction_reference"), get("settlement_reference"), strings.ToUpper(get("currency"))
		amount, parseErr := strconv.ParseInt(get("amount_minor"), 10, 64)
		settledAt, timeErr := time.Parse(time.RFC3339, get("settled_at"))
		if !validReference(reference) || amount < 0 || parseErr != nil || len(currency) != 3 || timeErr != nil || settledAt.After(time.Now().Add(24*time.Hour)) || len(settlementRef) > 100 {
			problem(w, 422, "INVALID_IMPORT", "A CSV row has an invalid reference, amount, currency, or settled_at timestamp.")
			return
		}
		var itemID *string
		match, matchErr := q.SettlementMatchForReference(r.Context(), store.SettlementMatchForReferenceParams{Environment: environment, Reference: reference})
		state := "unmatched"
		if matchErr == nil {
			itemID = &match.SettlementItemID
			// Serialize imports for this allocation across operator sessions.
			if err = q.LockSettlementItem(r.Context(), match.SettlementItemID); err != nil {
				problem(w, 503, "IMPORT_UNAVAILABLE", "Settlement matching could not be locked.")
				return
			}
			already, alreadyErr := q.SettlementItemAlreadyMatched(r.Context(), match.SettlementItemID)
			if alreadyErr != nil {
				problem(w, 503, "IMPORT_UNAVAILABLE", "Existing settlement records could not be checked.")
				return
			}
			switch {
			case already:
				state = "duplicate"
			case strings.TrimSpace(match.Currency) != currency:
				state = "currency_mismatch"
			case match.SellerEntitlementMinor != amount:
				state = "amount_mismatch"
			default:
				state = "matched"
			}
		} else if !errors.Is(matchErr, pgx.ErrNoRows) {
			problem(w, 503, "IMPORT_UNAVAILABLE", "Payment references could not be matched.")
			return
		}
		if err = q.InsertSettlementImportRow(r.Context(), store.InsertSettlementImportRowParams{ImportID: importID, Environment: environment, Reference: reference, SettlementReference: settlementRef, AmountMinor: amount, Currency: currency, SettledAt: settledAt, State: state, SettlementItemID: itemID}); err != nil {
			problem(w, 422, "DUPLICATE_IMPORT_ROW", "The file contains a repeated transaction reference or could not be stored.")
			return
		}
		// A manually supplied CSV is reconciliation input, never provider
		// confirmation. It cannot change the settlement or ledger state.
		switch state {
		case "matched":
			matched++
		case "unmatched":
			unmatched++
		case "amount_mismatch":
			amountMismatch++
		case "currency_mismatch":
			currencyMismatch++
		case "duplicate":
			duplicates++
		}
		if line > 5001 {
			problem(w, 422, "IMPORT_TOO_MANY_ROWS", "Settlement imports are limited to 5,000 rows.")
			return
		}
	}
	if matched+unmatched+amountMismatch+currencyMismatch+duplicates == 0 {
		problem(w, 422, "EMPTY_IMPORT", "Settlement import contains no data rows.")
		return
	}
	if err = q.AuditSettlementImport(r.Context(), store.AuditSettlementImportParams{ActorID: actor.ID, ImportID: importID, Matched: int32(matched), Unmatched: int32(unmatched), AmountMismatch: int32(amountMismatch), CurrencyMismatch: int32(currencyMismatch), Duplicates: int32(duplicates)}); err != nil {
		problem(w, 503, "AUDIT_REQUIRED", "The settlement import could not be audited.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "IMPORT_UNAVAILABLE", "Settlement import could not be committed.")
		return
	}
	jsonOut(w, 200, map[string]any{"import_id": importID, "matched": matched, "unmatched": unmatched, "amount_mismatch": amountMismatch, "currency_mismatch": currencyMismatch, "duplicates": duplicates, "source": "operator_supplied_csv", "settlement_confirmed": false})
}
