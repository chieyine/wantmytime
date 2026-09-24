package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Kora (korapay.com) collects payments (bank transfer first, card as a
// fallback) and pays sellers out of the same balance. Bank transfers settle
// into the balance instantly; cards settle the next working day.
//
// Amounts on Kora's API are in major units (naira, not kobo). WantMyTime keeps
// minor units everywhere else and converts only here.

const koraMaxResponse = 1 << 20

type koraClient struct {
	secret  string
	public  string
	baseURL string
	http    *http.Client
}

// newKoraClient fails closed on keys from the wrong environment.
func newKoraClient(appEnv string) (*koraClient, error) {
	secret := strings.TrimSpace(os.Getenv("KORA_SECRET_KEY"))
	public := strings.TrimSpace(os.Getenv("KORA_PUBLIC_KEY"))
	if secret == "" {
		return nil, errors.New("Kora secret key is not configured")
	}
	if appEnv == "production" {
		if !strings.HasPrefix(secret, "sk_live_") || (public != "" && !strings.HasPrefix(public, "pk_live_")) {
			return nil, errors.New("production requires live Kora keys")
		}
	} else if !strings.HasPrefix(secret, "sk_test_") || (public != "" && !strings.HasPrefix(public, "pk_test_")) {
		return nil, errors.New("non-production requires test Kora keys")
	}
	baseURL := "https://api.korapay.com/merchant"
	// KORA_API_BASE points non-production builds at a local fake provider for
	// integration tests. Production always talks to Kora directly.
	if override := strings.TrimRight(os.Getenv("KORA_API_BASE"), "/"); override != "" && appEnv != "production" {
		baseURL = override
	}
	return &koraClient{secret: secret, public: public, baseURL: baseURL, http: &http.Client{Timeout: 15 * time.Second}}, nil
}

// koraHTTPError is a non-2xx answer. 4xx (other than 429) means Kora refused
// the request; 5xx and network errors may or may not have taken effect.
type koraHTTPError struct {
	Status  int
	Message string
}

func (e *koraHTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("Kora returned HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("Kora returned HTTP %d", e.Status)
}

func providerRejected(err error) bool {
	var httpErr *koraHTTPError
	return errors.As(err, &httpErr) && httpErr.Status >= 400 && httpErr.Status < 500 && httpErr.Status != 429 && httpErr.Status != 404
}

func providerNotFound(err error) bool {
	var httpErr *koraHTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	msg := strings.ToLower(httpErr.Message)
	return httpErr.Status == 404 || (httpErr.Status == 400 && (strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist")))
}

func (k *koraClient) request(ctx context.Context, method, path string, body any, public bool, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.baseURL+path, reader)
	if err != nil {
		return err
	}
	key := k.secret
	if public && k.public != "" {
		key = k.public
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := k.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, koraMaxResponse+1))
	if err != nil {
		return err
	}
	if len(data) > koraMaxResponse {
		return errors.New("Kora response exceeded size limit")
	}
	var envelope struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &envelope)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &koraHTTPError{Status: resp.StatusCode, Message: envelope.Message}
	}
	if !envelope.Status {
		return &koraHTTPError{Status: 400, Message: envelope.Message}
	}
	if err = json.Unmarshal(data, out); err != nil {
		return errors.New("Kora response could not be decoded")
	}
	return nil
}

// --- amounts ------------------------------------------------------------------------

// majorAmount writes minor units as Kora's major-unit number: 1000000 kobo is
// 10000, 1050 kobo is 10.50.
func majorAmount(minor int64) json.Number {
	if minor%100 == 0 {
		return json.Number(strconv.FormatInt(minor/100, 10))
	}
	return json.Number(fmt.Sprintf("%d.%02d", minor/100, minor%100))
}

// koraMoney reads a major-unit amount that Kora may send as a number or a
// string ("100.00") into minor units, without floating point.
type koraMoney int64

func (m *koraMoney) UnmarshalJSON(b []byte) error {
	text := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if text == "" || text == "null" {
		*m = 0
		return nil
	}
	v, err := parseMajor(text)
	if err != nil {
		return err
	}
	*m = koraMoney(v)
	return nil
}

func parseMajor(text string) (int64, error) {
	neg := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	whole, frac, _ := strings.Cut(text, ".")
	if whole == "" {
		whole = "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > 1<<52 {
		return 0, fmt.Errorf("invalid amount %q", text)
	}
	for _, r := range frac {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid amount %q", text)
		}
	}
	cents := int64(0)
	switch {
	case len(frac) == 0:
	case len(frac) == 1:
		cents = int64(frac[0]-'0') * 10
	default:
		cents = int64(frac[0]-'0')*10 + int64(frac[1]-'0')
		if len(frac) > 2 && frac[2] >= '5' { // round half up
			cents++
		}
	}
	v := w*100 + cents
	if neg {
		v = -v
	}
	return v, nil
}

// --- collection -----------------------------------------------------------------------

type checkoutRequest struct {
	Reference   string
	AmountMinor int64
	Currency    string
	Email       string
	Name        string
	RedirectURL string
	WebhookURL  string
	Channels    []string
	Narration   string
	QuoteID     string
}

// bankTransferDetails is the one-off account the buyer transfers to.
type bankTransferDetails struct {
	AccountNumber string    `json:"account_number"`
	AccountName   string    `json:"account_name"`
	BankName      string    `json:"bank_name"`
	AmountMinor   int64     `json:"amount_minor"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// verifiedCharge is a charge as the provider reports it, in WantMyTime's terms.
type verifiedCharge struct {
	Reference string
	// Status is success, failed or pending.
	Status        string
	AmountMinor   int64 // the amount accepted for the booking
	PaidMinor     int64 // what the buyer actually sent
	Currency      string
	FeeMinor      int64 // provider fee including VAT
	Channel       string
	TransactionID string
	CardCountry   string
}

func (k *koraClient) startCheckout(ctx context.Context, in checkoutRequest) (string, error) {
	if in.AmountMinor <= 0 || in.Currency == "" || !validReference(in.Reference) || len(in.Channels) == 0 || !emailPattern.MatchString(in.Email) {
		return "", errors.New("invalid checkout request")
	}
	body := map[string]any{"amount": majorAmount(in.AmountMinor), "currency": in.Currency, "reference": in.Reference, "redirect_url": in.RedirectURL,
		"notification_url": in.WebhookURL, "narration": in.Narration, "channels": in.Channels, "default_channel": in.Channels[0], "merchant_bears_cost": true,
		"customer": map[string]string{"email": in.Email, "name": in.Name}, "metadata": map[string]string{"quote-id": strings.ReplaceAll(in.QuoteID, "-", "")}}
	var out struct {
		Data struct {
			Reference   string `json:"reference"`
			CheckoutURL string `json:"checkout_url"`
		} `json:"data"`
	}
	if err := k.request(ctx, http.MethodPost, "/api/v1/charges/initialize", body, false, &out); err != nil {
		return "", err
	}
	u, err := url.Parse(out.Data.CheckoutURL)
	if err != nil || u.Scheme != "https" || u.User != nil || !(u.Hostname() == "korapay.com" || strings.HasSuffix(u.Hostname(), ".korapay.com")) {
		return "", errors.New("Kora returned an unexpected checkout URL")
	}
	if out.Data.Reference != "" && out.Data.Reference != in.Reference {
		return "", errors.New("Kora returned a different reference")
	}
	return out.Data.CheckoutURL, nil
}

func (k *koraClient) startBankTransfer(ctx context.Context, in checkoutRequest) (bankTransferDetails, error) {
	if in.AmountMinor <= 0 || in.Currency != "NGN" || !validReference(in.Reference) || !emailPattern.MatchString(in.Email) {
		return bankTransferDetails{}, errors.New("invalid bank transfer request")
	}
	body := map[string]any{"amount": majorAmount(in.AmountMinor), "currency": in.Currency, "reference": in.Reference, "notification_url": in.WebhookURL,
		"narration": in.Narration, "merchant_bears_cost": true, "customer": map[string]string{"email": in.Email, "name": in.Name},
		"metadata": map[string]string{"quote-id": strings.ReplaceAll(in.QuoteID, "-", "")}}
	var out struct {
		Data struct {
			Reference      string    `json:"reference"`
			Amount         koraMoney `json:"amount"`
			AmountExpected koraMoney `json:"amount_expected"`
			BankAccount    struct {
				AccountName   string `json:"account_name"`
				AccountNumber string `json:"account_number"`
				BankName      string `json:"bank_name"`
				Expiry        string `json:"expiry_date_in_utc"`
			} `json:"bank_account"`
		} `json:"data"`
	}
	if err := k.request(ctx, http.MethodPost, "/api/v1/charges/bank-transfer", body, false, &out); err != nil {
		return bankTransferDetails{}, err
	}
	d := out.Data
	expires, err := time.Parse(time.RFC3339Nano, d.BankAccount.Expiry)
	if err != nil || d.Reference != in.Reference || d.BankAccount.AccountNumber == "" {
		return bankTransferDetails{}, errors.New("Kora did not return usable transfer details")
	}
	expected := int64(d.AmountExpected)
	if expected <= 0 {
		expected = int64(d.Amount)
	}
	if expected != in.AmountMinor {
		// merchant_bears_cost=true: the buyer must be asked for exactly the price.
		return bankTransferDetails{}, fmt.Errorf("Kora asked the buyer for %d instead of %d", expected, in.AmountMinor)
	}
	return bankTransferDetails{AccountNumber: d.BankAccount.AccountNumber, AccountName: d.BankAccount.AccountName, BankName: bankDisplayName(d.BankAccount.BankName), AmountMinor: expected, ExpiresAt: expires.UTC()}, nil
}

// bankDisplayName turns Kora's short names ("wema") into what buyers see in
// their banking app.
func bankDisplayName(name string) string {
	known := map[string]string{"wema": "Wema Bank", "sterling": "Sterling Bank", "providus": "Providus Bank", "fidelity": "Fidelity Bank", "gtb": "GTBank"}
	if full, ok := known[strings.ToLower(strings.TrimSpace(name))]; ok {
		return full
	}
	return name
}

func (k *koraClient) queryCharge(ctx context.Context, reference string) (verifiedCharge, error) {
	if !validReference(reference) {
		return verifiedCharge{}, errors.New("invalid reference")
	}
	var out struct {
		Data struct {
			Reference        string    `json:"reference"`
			PaymentReference string    `json:"payment_reference"`
			Status           string    `json:"status"`
			Amount           koraMoney `json:"amount"`
			AmountPaid       koraMoney `json:"amount_paid"`
			AmountAccepted   koraMoney `json:"amount_accepted"`
			Fee              koraMoney `json:"fee"`
			VAT              koraMoney `json:"vat"`
			Currency         string    `json:"currency"`
			PaymentMethod    string    `json:"payment_method"`
			Card             struct {
				Country string `json:"country"`
				Issuer  string `json:"issuer_country"`
			} `json:"card"`
		} `json:"data"`
	}
	if err := k.request(ctx, http.MethodGet, "/api/v1/charges/"+url.PathEscape(reference), nil, false, &out); err != nil {
		return verifiedCharge{}, err
	}
	d := out.Data
	if d.Reference != reference {
		return verifiedCharge{}, errors.New("Kora charge lookup did not match")
	}
	c := verifiedCharge{Reference: d.Reference, Currency: strings.ToUpper(d.Currency), FeeMinor: int64(d.Fee) + int64(d.VAT), Channel: strings.ToLower(d.PaymentMethod), TransactionID: firstNonEmpty(d.PaymentReference, d.Reference)}
	c.AmountMinor = int64(d.Amount)
	if d.AmountAccepted > 0 {
		c.AmountMinor = int64(d.AmountAccepted)
	}
	c.PaidMinor = int64(d.AmountPaid)
	if c.PaidMinor == 0 {
		c.PaidMinor = c.AmountMinor
	}
	if country := strings.ToUpper(firstNonEmpty(d.Card.Issuer, d.Card.Country)); len(country) == 2 {
		c.CardCountry = country
	}
	switch s := strings.ToLower(d.Status); s {
	case "success":
		c.Status = "success"
	case "failed", "expired", "cancelled", "abandoned":
		c.Status = "failed"
	default: // processing, pending: e.g. a transfer that is short of the price
		c.Status = "pending"
	}
	return c, nil
}

// --- refunds --------------------------------------------------------------------------

type providerRefund struct {
	Reference string
	// Status is success, failed or pending.
	Status string
}

func koraRefundStatus(s string) string {
	switch strings.ToLower(s) {
	case "success", "successful", "completed":
		return "success"
	case "failed":
		return "failed"
	}
	return "pending"
}

func (k *koraClient) refund(ctx context.Context, paymentReference, reference string, amountMinor int64, reason string) (providerRefund, error) {
	body := map[string]any{"payment_reference": paymentReference, "reference": reference, "amount": majorAmount(amountMinor), "reason": reason}
	var out struct {
		Data struct {
			Reference       string `json:"reference"`
			RefundReference string `json:"refund_reference"`
			Status          string `json:"status"`
		} `json:"data"`
	}
	if err := k.request(ctx, http.MethodPost, "/api/v1/refunds/initiate", body, false, &out); err != nil {
		return providerRefund{}, err
	}
	return providerRefund{Reference: reference, Status: koraRefundStatus(out.Data.Status)}, nil
}

func (k *koraClient) queryRefund(ctx context.Context, reference string) (providerRefund, bool, error) {
	var out struct {
		Data struct {
			Reference string `json:"reference"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	if err := k.request(ctx, http.MethodGet, "/api/v1/refunds/"+url.PathEscape(reference), nil, false, &out); err != nil {
		if providerNotFound(err) {
			return providerRefund{}, false, nil
		}
		return providerRefund{}, false, err
	}
	return providerRefund{Reference: reference, Status: koraRefundStatus(out.Data.Status)}, true, nil
}

// --- webhooks -------------------------------------------------------------------------

// validKoraSignature checks x-korapay-signature: HMAC-SHA256, keyed with the
// secret key, over the "data" object exactly as sent.
func validKoraSignature(raw []byte, signature, secret string) bool {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Data) == 0 || secret == "" {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	var compact bytes.Buffer
	candidates := [][]byte{envelope.Data}
	if json.Compact(&compact, envelope.Data) == nil {
		candidates = append(candidates, compact.Bytes())
	}
	for _, c := range candidates {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(c)
		if hmac.Equal(provided, mac.Sum(nil)) {
			return true
		}
	}
	return false
}

// validReference allows the characters every provider accepts.
func validReference(reference string) bool {
	if len(reference) < 8 || len(reference) > 50 {
		return false
	}
	for _, r := range reference {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
