package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// A market is a country where sellers can be based: it fixes the currency
// they are priced, charged and paid in, how buyers can pay in that currency,
// and where payouts can go. WantMyTime takes no card payments: buyers pay by
// bank transfer or pay with bank in Nigeria, and by mobile money in Ghana and
// Kenya.
//
// WantMyTime starts in Nigeria. Other markets are built in and switched on
// with SELLER_COUNTRIES (for example "NG,GH,KE") once Kora has enabled
// collection and payouts for them on the merchant account.
type market struct {
	Country  string `json:"country"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
	// Timezone is the usual zone, used to guess a new seller's country.
	Timezone string `json:"timezone"`
	// Channels are the ways a buyer can pay in this currency through Kora,
	// most common first. APPROVED_PAYMENT_CHANNELS(_<CURRENCY>) picks from these.
	Channels []string `json:"-"`
	// AccountDigits is the exact bank account length (0: 6 to 20 digits).
	AccountDigits int `json:"account_digits"`
	// BankPayouts: sellers can be paid into a bank account here.
	BankPayouts bool `json:"bank_payouts"`
	// NameCheck: the bank confirms the account holder's name before it is
	// saved. Elsewhere the seller types the name on the account.
	NameCheck bool `json:"name_check"`
	// MobileMoney lists the wallets sellers can be paid into, if any.
	MobileMoney []mobileOperator `json:"mobile_money"`
	// PhonePrefix is the international dialling code for wallet numbers.
	PhonePrefix string `json:"phone_prefix,omitempty"`
}

type mobileOperator struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// allMarkets are the markets the code supports. Kora's operator codes and
// channel names were taken from its public documentation; confirm them in
// the sandbox before switching a market on.
var allMarkets = []market{
	{Country: "NG", Name: "Nigeria", Currency: "NGN", Timezone: "Africa/Lagos", Channels: []string{"bank_transfer", "pay_with_bank"}, AccountDigits: 10, BankPayouts: true, NameCheck: true},
	{Country: "GH", Name: "Ghana", Currency: "GHS", Timezone: "Africa/Accra", Channels: []string{"mobile_money"}, BankPayouts: true, PhonePrefix: "233",
		MobileMoney: []mobileOperator{{"mtn-gh", "MTN MoMo"}, {"vodafone-gh", "Telecel Cash"}, {"airteltigo-gh", "AirtelTigo Money"}}},
	{Country: "KE", Name: "Kenya", Currency: "KES", Timezone: "Africa/Nairobi", Channels: []string{"mobile_money"}, BankPayouts: true, PhonePrefix: "254",
		MobileMoney: []mobileOperator{{"safaricom-ke", "M-Pesa"}, {"airtel-ke", "Airtel Money"}}},
}

// supportedChannels are every payment channel the checkout knows how to start.
// Cards are deliberately not among them.
var supportedChannels = map[string]bool{"bank_transfer": true, "pay_with_bank": true, "mobile_money": true}

// enabledMarkets are the markets switched on, in the order configured
// (Nigeria alone by default).
func enabledMarkets() []market {
	raw := strings.TrimSpace(os.Getenv("SELLER_COUNTRIES"))
	if raw == "" {
		raw = "NG"
	}
	out := []market{}
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		code := strings.ToUpper(strings.TrimSpace(item))
		if seen[code] {
			continue
		}
		for _, m := range allMarkets {
			if m.Country == code {
				out = append(out, m)
				seen[code] = true
			}
		}
	}
	if len(out) == 0 {
		out = append(out, allMarkets[0])
	}
	return out
}

// marketFor returns an enabled market by country code.
func marketFor(country string) (market, bool) {
	country = strings.ToUpper(strings.TrimSpace(country))
	for _, m := range enabledMarkets() {
		if m.Country == country {
			return m, true
		}
	}
	return market{}, false
}

// marketForCurrency returns the market that uses a currency, enabled or
// not: existing records keep working after a market is switched off.
func marketForCurrency(currency string) (market, bool) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	for _, m := range allMarkets {
		if m.Currency == currency {
			return m, true
		}
	}
	return market{}, false
}

func (m market) operatorName(code string) (string, bool) {
	for _, op := range m.MobileMoney {
		if op.Code == code {
			return op.Name, true
		}
	}
	return "", false
}

// approvedChannelsFor lists how buyers may pay in a currency, bank transfer
// first when it is approved (nearly every Nigerian pays by transfer, and
// transfers settle at once). APPROVED_PAYMENT_CHANNELS_<CURRENCY> sets the
// list; for NGN, plain APPROVED_PAYMENT_CHANNELS is read as well.
func approvedChannelsFor(currency string) ([]string, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	raw := os.Getenv("APPROVED_PAYMENT_CHANNELS_" + currency)
	if raw == "" && currency == "NGN" {
		raw = os.Getenv("APPROVED_PAYMENT_CHANNELS")
	}
	m, known := marketForCurrency(currency)
	out := []string{}
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		ch := strings.TrimSpace(item)
		if ch == "" {
			continue
		}
		if !supportedChannels[ch] || (known && !contains(m.Channels, ch)) {
			return nil, fmt.Errorf("payment channel %q is not supported for %s", ch, currency)
		}
		if !seen[ch] {
			seen[ch] = true
			out = append(out, ch)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("approved payment channels are required for " + currency)
	}
	for i, ch := range out {
		if ch == "bank_transfer" && i > 0 {
			out = append([]string{"bank_transfer"}, append(out[:i:i], out[i+1:]...)...)
			break
		}
	}
	return out, nil
}

// approvedChannels is the Nigerian list, kept for the original checks.
func approvedChannels() ([]string, error) { return approvedChannelsFor("NGN") }

// chargeLimits are the smallest and largest payments accepted in a currency,
// in minor units: MIN_CHARGE_MINOR_<CURRENCY> and MAX_CHARGE_MINOR_<CURRENCY>
// (plain MIN_CHARGE_MINOR and MAX_CHARGE_MINOR for NGN).
func chargeLimits(currency string) (int64, int64, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	read := func(name string) string {
		if v := os.Getenv(name + "_" + currency); v != "" {
			return v
		}
		if currency == "NGN" {
			return os.Getenv(name)
		}
		return ""
	}
	min, minErr := strconv.ParseInt(read("MIN_CHARGE_MINOR"), 10, 64)
	max, maxErr := strconv.ParseInt(read("MAX_CHARGE_MINOR"), 10, 64)
	if minErr != nil || maxErr != nil || min <= 0 || max < min {
		return 0, 0, errors.New("payment limits are not configured for " + currency)
	}
	return min, max, nil
}

// checkoutReadyFor reports whether buyers can pay in a currency right now.
func (a *API) checkoutReadyFor(currency string) bool {
	if !a.providerCheckoutConfigured() {
		return false
	}
	if _, err := approvedChannelsFor(currency); err != nil {
		return false
	}
	_, _, err := chargeLimits(currency)
	return err == nil
}

// paymentMethodsFor lists the ways a buyer can pay a seller in a currency.
func paymentMethodsFor(currency string) []string {
	methods, err := approvedChannelsFor(currency)
	if err != nil {
		return []string{}
	}
	return methods
}

// guessCountry picks a likely country for a new seller from their timezone.
func guessCountry(timezone string) string {
	enabled := enabledMarkets()
	for _, m := range enabled {
		if m.Timezone == timezone {
			return m.Country
		}
	}
	return enabled[0].Country
}

// publicMarkets lists the countries sellers can sign up from.
func (a *API) publicMarkets(w http.ResponseWriter, r *http.Request) {
	jsonOut(w, 200, map[string]any{"markets": enabledMarkets()})
}
