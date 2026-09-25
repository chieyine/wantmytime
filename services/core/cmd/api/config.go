package main

import (
	"encoding/base64"
	"net/url"
	"os"
	"strings"
)

// productionConfigProblems checks the environment before a production API
// starts. Problems stop the process; warnings are logged. Every secret is
// checked for presence, strength and reuse, since one leaked value must never
// unlock two things.
func productionConfigProblems(getenv func(string) string) (problems, warnings []string) {
	weak := func(v string) bool {
		l := strings.ToLower(v)
		return strings.Contains(l, "change") || strings.Contains(l, "local-only") || strings.Contains(l, "example") || strings.Contains(l, "secret-here")
	}
	seen := map[string]string{}
	remember := func(name, value string) {
		if value == "" {
			return
		}
		if other, ok := seen[value]; ok {
			problems = append(problems, name+" reuses the value of "+other+"; give every secret its own value")
			return
		}
		seen[value] = name
	}
	for _, name := range []string{"SESSION_SECRET", "OTP_PEPPER"} {
		v := getenv(name)
		switch {
		case len(v) < 32:
			problems = append(problems, name+" must be at least 32 random characters")
		case weak(v):
			problems = append(problems, name+" still holds a placeholder value")
		}
		remember(name, v)
	}
	keys := []string{"OPS_MFA_ENCRYPTION_KEY", "MEETING_LINK_ENCRYPTION_KEY", "PAYOUT_ACCOUNT_ENCRYPTION_KEY"}
	if getenv("GOOGLE_CLIENT_ID") != "" {
		keys = append(keys, "CALENDAR_TOKEN_ENCRYPTION_KEY")
	}
	for _, name := range keys {
		v := getenv(name)
		if key, err := base64.StdEncoding.DecodeString(v); err != nil || len(key) != 32 {
			problems = append(problems, name+" must be the base64 encoding of 32 random bytes (openssl rand -base64 32)")
		}
		remember(name, v)
	}
	if origin, err := url.Parse(getenv("PUBLIC_APP_ORIGIN")); err != nil || origin.Scheme != "https" || origin.Host == "" || origin.Path != "" {
		problems = append(problems, "PUBLIC_APP_ORIGIN must be the https origin people use, such as https://wantmytime.com")
	}
	for _, name := range []string{"ALLOW_LOG_OTP", "LOCAL_PAYMENT_SIMULATOR"} {
		if getenv(name) == "true" {
			problems = append(problems, name+" must not be true in production")
		}
	}
	for _, name := range []string{"KORA_API_BASE", "GOOGLE_API_BASE", "GOOGLE_AUTH_BASE", "GOOGLE_OAUTH_BASE"} {
		if getenv(name) != "" {
			problems = append(problems, name+" is a test override and must be unset in production")
		}
	}
	if t := getenv("METRICS_TOKEN"); t != "" && len(t) < 24 {
		problems = append(problems, "METRICS_TOKEN must be at least 24 random characters")
	}
	if r := getenv("REDIS_URL"); r == "" {
		warnings = append(warnings, "REDIS_URL is not set: rate limits are counted per API instance, not shared")
	} else if _, err := newRedisClient(r); err != nil {
		problems = append(problems, err.Error())
	}
	if getenv("VAPID_PUBLIC_KEY") == "" || getenv("VAPID_PRIVATE_KEY") == "" {
		warnings = append(warnings, "VAPID keys are not set: phone and browser notifications are off (run `aside-api vapid-keys`)")
	}
	if getenv("MEDIA_S3_ENDPOINT") == "" {
		warnings = append(warnings, "MEDIA_S3_ENDPOINT is not set: profile photos are stored in PostgreSQL")
	}
	if db, err := url.Parse(getenv("DATABASE_URL")); err == nil {
		mode := db.Query().Get("sslmode")
		host := db.Hostname()
		private := host == "localhost" || host == "127.0.0.1" || host == "db" || !strings.Contains(host, ".")
		if !private && mode != "require" && mode != "verify-ca" && mode != "verify-full" {
			warnings = append(warnings, "DATABASE_URL does not require TLS (add sslmode=verify-full for a managed database)")
		}
	}
	for _, item := range strings.Split(getenv("SELLER_COUNTRIES"), ",") {
		code := strings.ToUpper(strings.TrimSpace(item))
		if code == "" {
			continue
		}
		known := false
		for _, m := range allMarkets {
			known = known || m.Country == code
		}
		if !known {
			problems = append(problems, "SELLER_COUNTRIES lists "+code+", which WantMyTime does not support")
		}
	}
	if getenv("SENTRY_DSN") == "" {
		warnings = append(warnings, "SENTRY_DSN is not set: server errors are only in the logs")
	}
	return problems, warnings
}

func envLookup(name string) string { return os.Getenv(name) }
