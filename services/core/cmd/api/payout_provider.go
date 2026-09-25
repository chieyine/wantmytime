package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// payoutProvider is everything WantMyTime needs from a payments company to pay
// sellers: find the bank list, check an account's name and send money to it.
// Kora is the implementation today; a provider for another country plugs in
// here without touching bookings, refunds or the ledger.
type payoutProvider interface {
	name() string
	banks(ctx context.Context, country string) ([]payoutBank, error)
	resolveAccount(ctx context.Context, country, bankCode, accountNumber string) (string, error)
	// transfer sends money. reference is WantMyTime's and is unique per attempt;
	// sending the same reference twice must never pay twice.
	transfer(ctx context.Context, in payoutTransferRequest) (payoutTransfer, error)
	// findTransfer looks a transfer up by WantMyTime's reference. found is false
	// when the provider has never seen the reference.
	findTransfer(ctx context.Context, reference string) (payoutTransfer, bool, error)
}

type payoutBank struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type payoutTransferRequest struct {
	// DestinationType is bank_account or mobile_money. For mobile money,
	// BankCode is the network operator and AccountNumber the wallet number.
	DestinationType string
	BankCode        string
	AccountNumber   string
	AccountName     string
	Email           string
	Reference       string
	Reason          string
	Currency        string
	AmountMinor     int64
}

type payoutTransfer struct {
	Reference string
	// Status is normalized: success, pending, failed or reversed.
	Status   string
	Amount   int64
	FeeMinor int64
	Message  string
}

var (
	// errPayoutFundsPending means the balance does not yet hold the money:
	// the buyer's payment has not settled. The payout waits and tries again;
	// nothing is pre-funded.
	errPayoutFundsPending = errors.New("payout balance does not yet hold the funds")
	// errPayoutRejected means the provider refused the request outright
	// (bad account, invalid details); repeating it will not help.
	errPayoutRejected = errors.New("payout rejected by provider")
)

func (a *API) payoutProvider() (payoutProvider, error) {
	client, err := newKoraClient(a.env)
	if err != nil {
		return nil, err
	}
	return koraPayouts{client}, nil
}

// --- Kora -------------------------------------------------------------------------------

type koraPayouts struct{ c *koraClient }

func (koraPayouts) name() string { return "kora" }

func (p koraPayouts) banks(ctx context.Context, country string) ([]payoutBank, error) {
	var out struct {
		Data []struct {
			Name string `json:"name"`
			Code string `json:"code"`
		} `json:"data"`
	}
	if err := p.c.request(ctx, http.MethodGet, "/api/v1/misc/banks?countryCode="+url.QueryEscape(country), nil, true, &out); err != nil {
		return nil, err
	}
	banks := []payoutBank{}
	seen := map[string]bool{}
	for _, b := range out.Data {
		if b.Code == "" || b.Name == "" || seen[b.Code] {
			continue
		}
		seen[b.Code] = true
		banks = append(banks, payoutBank{Code: b.Code, Name: b.Name})
	}
	return banks, nil
}

func (p koraPayouts) resolveAccount(ctx context.Context, country, bankCode, accountNumber string) (string, error) {
	var out struct {
		Data struct {
			AccountName string `json:"account_name"`
		} `json:"data"`
	}
	// Kora names the account's currency here, not its country.
	currency := "NGN"
	if m, ok := marketFor(country); ok {
		currency = m.Currency
	}
	body := map[string]string{"bank": bankCode, "account": accountNumber, "currency": currency}
	if err := p.c.request(ctx, http.MethodPost, "/api/v1/misc/banks/resolve", body, true, &out); err != nil {
		if providerRejected(err) || providerNotFound(err) {
			return "", errPayoutRejected
		}
		return "", err
	}
	name := strings.TrimSpace(out.Data.AccountName)
	if name == "" {
		return "", errPayoutRejected
	}
	return name, nil
}

type koraTransferData struct {
	Reference string    `json:"reference"`
	Status    string    `json:"status"`
	Amount    koraMoney `json:"amount"`
	Fee       koraMoney `json:"fee"`
	Message   string    `json:"message"`
}

func (d koraTransferData) normalized() payoutTransfer {
	out := payoutTransfer{Reference: d.Reference, Amount: int64(d.Amount), FeeMinor: int64(d.Fee)}
	switch s := strings.ToLower(strings.TrimSpace(d.Status)); s {
	case "success", "successful":
		out.Status = "success"
	case "failed":
		out.Status, out.Message = "failed", firstNonEmpty(d.Message, "Kora reported the transfer as failed")
	case "reversed":
		out.Status, out.Message = "reversed", "The bank returned the transfer"
	default: // processing, pending, delayed, unavailable
		out.Status = "pending"
	}
	return out
}

func (p koraPayouts) transfer(ctx context.Context, in payoutTransferRequest) (payoutTransfer, error) {
	if in.AmountMinor <= 0 || in.AccountNumber == "" || in.BankCode == "" || !validReference(in.Reference) {
		return payoutTransfer{}, errors.New("invalid transfer request")
	}
	destination := map[string]any{
		"type": "bank_account", "amount": majorAmount(in.AmountMinor), "currency": in.Currency, "narration": in.Reason,
		"bank_account": map[string]string{"bank": in.BankCode, "account": in.AccountNumber},
		"customer":     map[string]string{"name": in.AccountName, "email": in.Email},
	}
	if in.DestinationType == "mobile_money" {
		delete(destination, "bank_account")
		destination["type"] = "mobile_money"
		destination["mobile_money"] = map[string]string{"operator": in.BankCode, "mobile_number": in.AccountNumber}
	}
	body := map[string]any{"reference": in.Reference, "destination": destination}
	var out struct {
		Data koraTransferData `json:"data"`
	}
	if err := p.c.request(ctx, http.MethodPost, "/api/v1/transactions/disburse", body, false, &out); err != nil {
		var httpErr *koraHTTPError
		if errors.As(err, &httpErr) {
			msg := strings.ToLower(httpErr.Message)
			if strings.Contains(msg, "insufficient") || strings.Contains(msg, "balance") {
				return payoutTransfer{}, errPayoutFundsPending
			}
		}
		if providerRejected(err) {
			return payoutTransfer{}, fmt.Errorf("%w: %s", errPayoutRejected, err.Error())
		}
		return payoutTransfer{}, err
	}
	if out.Data.Reference != "" && out.Data.Reference != in.Reference {
		return payoutTransfer{}, errors.New("Kora did not confirm the transfer")
	}
	t := out.Data.normalized()
	t.Reference = in.Reference
	return t, nil
}

func (p koraPayouts) findTransfer(ctx context.Context, reference string) (payoutTransfer, bool, error) {
	var out struct {
		Data koraTransferData `json:"data"`
	}
	if err := p.c.request(ctx, http.MethodGet, "/api/v1/transactions/"+url.PathEscape(reference), nil, false, &out); err != nil {
		if providerNotFound(err) {
			return payoutTransfer{}, false, nil
		}
		return payoutTransfer{}, false, err
	}
	if out.Data.Reference != reference {
		return payoutTransfer{}, false, errors.New("Kora transfer lookup did not match")
	}
	return out.Data.normalized(), true, nil
}

// --- bank list cache -----------------------------------------------------------------

type cachedBanks struct {
	banks   []payoutBank
	fetched time.Time
}

var bankCache struct {
	sync.Mutex
	byCountry map[string]cachedBanks
}

func (a *API) payoutBanks(ctx context.Context, country string) ([]payoutBank, error) {
	bankCache.Lock()
	defer bankCache.Unlock()
	if c, ok := bankCache.byCountry[country]; ok && len(c.banks) > 0 && time.Since(c.fetched) < 12*time.Hour {
		return c.banks, nil
	}
	provider, err := a.payoutProvider()
	if err != nil {
		return nil, err
	}
	banks, err := provider.banks(ctx, country)
	if err != nil {
		return nil, err
	}
	if bankCache.byCountry == nil {
		bankCache.byCountry = map[string]cachedBanks{}
	}
	bankCache.byCountry[country] = cachedBanks{banks, time.Now()}
	return banks, nil
}

// The countries sellers can be paid in, with their currency and account
// rules, are the enabled markets (markets.go).

// --- timing --------------------------------------------------------------------------

// disputeWindow is how long after the session ends the buyer can still report
// a problem or a no-show (default two hours).
func disputeWindow() time.Duration {
	return minutesFromEnv("DISPUTE_WINDOW_MINUTES", 120, 30, 7*24*60)
}

// payoutDelay is when, after the session ends, the seller is paid (default
// three hours). It always leaves time after the dispute window closes.
func payoutDelay() time.Duration {
	d := minutesFromEnv("PAYOUT_DELAY_MINUTES", 180, 30, 14*24*60)
	if floor := disputeWindow() + 30*time.Minute; d < floor {
		return floor
	}
	return d
}

func minutesFromEnv(key string, def, lo, hi int) time.Duration {
	var v int
	if _, err := fmt.Sscan(os.Getenv(key), &v); err != nil || v < lo || v > hi {
		v = def
	}
	return time.Duration(v) * time.Minute
}

// payoutsPaused stops new transfers (an emergency switch). Scheduled payouts
// wait and go out once it is lifted.
func payoutsPaused() bool { return os.Getenv("PAYOUTS_PAUSED") == "true" }

// payoutAccountCooldown delays payouts into a bank account that replaced an
// earlier one.
const payoutAccountCooldown = 24 * time.Hour
