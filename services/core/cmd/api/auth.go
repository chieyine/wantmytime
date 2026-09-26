package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	_ "image/jpeg"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func digestFromRequest(r *http.Request) []byte {
	c, _ := r.Cookie("aside_session")
	return digest(c.Value)
}

var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func validEmail(v string) bool {
	return len(v) <= 254 && emailPattern.MatchString(strings.TrimSpace(v))
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

func digest(s string) []byte { d := sha256.Sum256([]byte(s)); return d[:] }

func analyticsSubjectHash(subject string) []byte {
	key := os.Getenv("OTP_PEPPER")
	if key == "" {
		key = envOr("SESSION_SECRET", "local-only-session-secret-change-before-use")
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(subject))
	return mac.Sum(nil)
}

type challengeRequest struct {
	Email   string `json:"email"`
	Purpose string `json:"purpose"`
}

func (a *API) createChallenge(w http.ResponseWriter, r *http.Request) {
	var in challengeRequest
	if decode(r, &in) != nil || !validEmail(in.Email) {
		problem(w, 422, "INVALID_EMAIL", "Enter a valid email address.")
		return
	}
	if in.Purpose != "login" && in.Purpose != "claim" && in.Purpose != "guest_access" && in.Purpose != "guest_booking" && in.Purpose != "guest_offer" {
		problem(w, 422, "INVALID_PURPOSE", "Choose a supported verification purpose.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	code, e := randomDigits(8)
	if e != nil {
		problem(w, 500, "CHALLENGE_ERROR", "A verification code could not be created.")
		return
	}
	id, e := randomUUID()
	if e != nil {
		problem(w, 500, "CHALLENGE_ERROR", "A verification code could not be created.")
		return
	}
	pepper := os.Getenv("OTP_PEPPER")
	if pepper == "" {
		if a.env == "production" {
			problem(w, 503, "EMAIL_DISABLED", "Email verification is not configured.")
			return
		}
		pepper = string(a.sessionKey)
	}
	var recent int
	tx, e := a.db.Begin(r.Context())
	if e != nil {
		problem(w, 503, "CHALLENGE_ERROR", "A verification code could not be created.")
		return
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "challenge:"+email); e != nil {
		problem(w, 503, "CHALLENGE_ERROR", "A verification code could not be created.")
		return
	}
	if e = tx.QueryRow(r.Context(), `SELECT count(*) FROM email_challenges WHERE normalized_email=$1 AND created_at>now()-interval '1 hour'`, email).Scan(&recent); e != nil {
		problem(w, 503, "CHALLENGE_ERROR", "A verification code could not be created.")
		return
	}
	if recent >= 5 {
		problem(w, 429, "RATE_LIMITED", "Too many verification requests. Try again later.")
		return
	}
	_, e = tx.Exec(r.Context(), `INSERT INTO email_challenges(id,normalized_email,purpose,code_hash,expires_at) VALUES($1,$2,$3,$4,now()+interval '10 minutes')`, id, email, in.Purpose, digest(pepper+":"+id+":"+code))
	if e != nil {
		problem(w, 503, "CHALLENGE_ERROR", "A verification code could not be saved.")
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		problem(w, 503, "CHALLENGE_ERROR", "A verification code could not be saved.")
		return
	}
	if !a.sendCode(email, code) {
		_, _ = a.db.Exec(r.Context(), `DELETE FROM email_challenges WHERE id=$1`, id)
		problem(w, 503, "EMAIL_DISABLED", "Email delivery is not configured. No code was sent.")
		return
	}
	jsonOut(w, 202, map[string]any{"challenge_id": id, "expires_in_seconds": 600, "message": "If this address can be used, a code was sent."})
}

func randomDigits(n int) (string, error) {
	// Rejection sampling: bytes >= 250 are discarded so every digit is equally likely.
	out := make([]byte, 0, n)
	buf := make([]byte, n*2)
	for len(out) < n {
		if _, e := rand.Read(buf); e != nil {
			return "", e
		}
		for _, v := range buf {
			if v < 250 {
				out = append(out, '0'+v%10)
				if len(out) == n {
					break
				}
			}
		}
	}
	return string(out), nil
}

func (a *API) sendCode(email, code string) bool {
	content := emailContent{
		Subject:    "Your WantMyTime sign-in code",
		Preheader:  "Your code is " + code + ". It works for 10 minutes.",
		Heading:    "Your sign-in code",
		Paragraphs: []string{"Enter this code on WantMyTime to continue. It works for 10 minutes."},
		Code:       code,
		Notes:      []string{"Didn’t ask for a code? You can ignore this email. Nobody can sign in without it."},
		Footer:     "You’re getting this because someone entered this address on WantMyTime.",
	}
	if a.deliverEmail(context.Background(), content.message(email, "")) {
		return true
	}
	if a.env != "production" && os.Getenv("ALLOW_LOG_OTP") == "true" {
		log.Printf("LOCAL OTP email_hash=%s code=%s", hex.EncodeToString(digest(email))[:12], code)
		return true
	}
	return false
}

func (a *API) verifyChallenge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
		// Timezone is the browser's IANA zone. It is optional and only used to
		// show times in emails in the person's own zone.
		Timezone string `json:"timezone"`
	}
	if decode(r, &in) != nil || len(in.Code) != 8 {
		problem(w, 422, "INVALID_CODE", "Enter the eight digit code.")
		return
	}
	id := r.PathValue("id")
	pepper := os.Getenv("OTP_PEPPER")
	if pepper == "" {
		if a.env == "production" {
			problem(w, 503, "EMAIL_DISABLED", "Email verification is not configured.")
			return
		}
		pepper = string(a.sessionKey)
	}
	tx, e := a.db.Begin(r.Context())
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Verification is unavailable.")
		return
	}
	defer tx.Rollback(r.Context())
	var email, purpose string
	var expected []byte
	var attempts int
	e = tx.QueryRow(r.Context(), `SELECT normalized_email,purpose,code_hash,attempts FROM email_challenges WHERE id=$1 AND consumed_at IS NULL AND expires_at>now() FOR UPDATE`, id).Scan(&email, &purpose, &expected, &attempts)
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 400, "INVALID_OR_EXPIRED_CODE", "That code is invalid or expired.")
		return
	}
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Verification is unavailable.")
		return
	}
	actual := digest(pepper + ":" + id + ":" + in.Code)
	if attempts >= 5 || subtle.ConstantTimeCompare(expected, actual) != 1 {
		if _, e = tx.Exec(r.Context(), `UPDATE email_challenges SET attempts=LEAST(attempts+1,5) WHERE id=$1`, id); e != nil {
			problem(w, 503, "DATABASE_ERROR", "The verification attempt could not be recorded.")
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			problem(w, 503, "DATABASE_ERROR", "The verification attempt could not be recorded.")
			return
		}
		problem(w, 400, "INVALID_OR_EXPIRED_CODE", "That code is invalid or expired.")
		return
	}
	if _, e = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, email); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Verification is unavailable.")
		return
	}
	var userID string
	e = tx.QueryRow(r.Context(), `SELECT user_id::text FROM user_identities WHERE type='email' AND normalized_identifier=$1`, email).Scan(&userID)
	if errors.Is(e, pgx.ErrNoRows) {
		e = tx.QueryRow(r.Context(), `INSERT INTO users(id,display_name) VALUES(gen_random_uuid(),split_part($1,'@',1)) RETURNING id::text`, email).Scan(&userID)
	}
	if e != nil {
		problem(w, 503, "ACCOUNT_ERROR", "Account could not be loaded.")
		return
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO user_identities(id,user_id,type,normalized_identifier,verified_at) VALUES(gen_random_uuid(),$1,'email',$2,now()) ON CONFLICT(type,normalized_identifier) DO UPDATE SET verified_at=COALESCE(user_identities.verified_at,now())`, userID, email); e != nil {
		problem(w, 503, "ACCOUNT_ERROR", "Identity could not be verified.")
		return
	}
	if _, e = tx.Exec(r.Context(), `UPDATE email_challenges SET consumed_at=now() WHERE id=$1`, id); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Verification could not be completed.")
		return
	}
	// A sign-in code proves the address, so an earlier yes to announcements now counts.
	if e = confirmMarketingEmail(r.Context(), tx, email); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Verification could not be completed.")
		return
	}
	if zone, zoneErr := loadNamedTimezone(strings.TrimSpace(in.Timezone)); zoneErr == nil {
		if _, e = tx.Exec(r.Context(), `UPDATE users SET timezone=$2 WHERE id=$1 AND timezone IS DISTINCT FROM $2`, userID, zone.String()); e != nil {
			problem(w, 503, "ACCOUNT_ERROR", "Account could not be updated.")
			return
		}
	}
	// A signed-in account that verifies its own email for a guest flow keeps its
	// full session; replacing the cookie would sign the person out of /app.
	if strings.HasPrefix(purpose, "guest_") {
		if current, currentErr := a.currentUser(r); currentErr == nil && current.ID == userID {
			if e = tx.Commit(r.Context()); e != nil {
				problem(w, 503, "DATABASE_ERROR", "Verification could not be completed.")
				return
			}
			jsonOut(w, 200, map[string]any{"verified": true, "purpose": purpose, "booking_ids": []string{}, "session": "existing"})
			return
		}
	}
	token, e := randomToken(32)
	if e != nil {
		problem(w, 500, "SESSION_ERROR", "A session could not be created.")
		return
	}
	var accessibleBookingIDs []string
	if purpose == "guest_access" {
		scope := map[string]any{"purpose": purpose, "quote_ids": []string{}, "offer_ids": []string{}, "booking_ids": []string{}}
		rows, queryErr := tx.Query(r.Context(), `SELECT id::text,quote_id::text FROM bookings WHERE buyer_user_id=$1`, userID)
		if queryErr != nil {
			problem(w, 503, "SESSION_ERROR", "A restricted booking session could not be created.")
			return
		}
		for rows.Next() {
			var bookingID string
			var quoteID *string
			if scanErr := rows.Scan(&bookingID, &quoteID); scanErr != nil {
				rows.Close()
				problem(w, 503, "SESSION_ERROR", "A restricted booking session could not be created.")
				return
			}
			scope["booking_ids"] = append(scope["booking_ids"].([]string), bookingID)
			accessibleBookingIDs = append(accessibleBookingIDs, bookingID)
			if quoteID != nil {
				scope["quote_ids"] = append(scope["quote_ids"].([]string), *quoteID)
			}
		}
		if rows.Err() != nil {
			rows.Close()
			problem(w, 503, "SESSION_ERROR", "A restricted booking session could not be created.")
			return
		}
		rows.Close()
		// An email-verified buyer must be able to resume a provider payment
		// even when the browser session was lost before booking conversion.
		rows, queryErr = tx.Query(r.Context(), `SELECT DISTINCT q.id::text FROM quotes q JOIN payment_attempts p ON p.quote_id=q.id WHERE q.buyer_user_id=$1`, userID)
		if queryErr != nil {
			problem(w, 503, "SESSION_ERROR", "A restricted payment session could not be created.")
			return
		}
		for rows.Next() {
			var quoteID string
			if scanErr := rows.Scan(&quoteID); scanErr != nil {
				rows.Close()
				problem(w, 503, "SESSION_ERROR", "A restricted payment session could not be created.")
				return
			}
			found := false
			for _, existing := range scope["quote_ids"].([]string) {
				if existing == quoteID {
					found = true
					break
				}
			}
			if !found {
				scope["quote_ids"] = append(scope["quote_ids"].([]string), quoteID)
			}
		}
		if rows.Err() != nil {
			rows.Close()
			problem(w, 503, "SESSION_ERROR", "A restricted payment session could not be created.")
			return
		}
		rows.Close()
		rows, queryErr = tx.Query(r.Context(), `SELECT id::text FROM offers WHERE buyer_user_id=$1`, userID)
		if queryErr != nil {
			problem(w, 503, "SESSION_ERROR", "A restricted offer session could not be created.")
			return
		}
		for rows.Next() {
			var offerID string
			if scanErr := rows.Scan(&offerID); scanErr != nil {
				rows.Close()
				problem(w, 503, "SESSION_ERROR", "A restricted offer session could not be created.")
				return
			}
			scope["offer_ids"] = append(scope["offer_ids"].([]string), offerID)
		}
		if rows.Err() != nil {
			rows.Close()
			problem(w, 503, "SESSION_ERROR", "A restricted offer session could not be created.")
			return
		}
		rows.Close()
		scopeJSON, _ := json.Marshal(scope)
		_, e = tx.Exec(r.Context(), `INSERT INTO sessions(id,token_hash,user_id,guest_scope,expires_at) VALUES(gen_random_uuid(),$1,$2,$3,now()+interval '30 days')`, digest(token), userID, scopeJSON)
	} else if purpose == "guest_booking" || purpose == "guest_offer" {
		scopeJSON, _ := json.Marshal(map[string]any{"purpose": purpose, "quote_ids": []string{}, "offer_ids": []string{}, "booking_ids": []string{}})
		_, e = tx.Exec(r.Context(), `INSERT INTO sessions(id,token_hash,user_id,guest_scope,expires_at) VALUES(gen_random_uuid(),$1,$2,$3,now()+interval '30 days')`, digest(token), userID, scopeJSON)
	} else {
		_, e = tx.Exec(r.Context(), `INSERT INTO sessions(id,token_hash,user_id,expires_at) VALUES(gen_random_uuid(),$1,$2,now()+interval '30 days')`, digest(token), userID)
	}
	if e != nil {
		problem(w, 503, "SESSION_ERROR", "A session could not be created.")
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Verification could not be completed.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "aside_session", Value: token, Path: "/", HttpOnly: true, Secure: a.env == "production", SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 60 * 60})
	jsonOut(w, 200, map[string]any{"verified": true, "purpose": purpose, "booking_ids": accessibleBookingIDs})
}

type user struct {
	ID    string
	Email string
}

func (a *API) currentUser(r *http.Request) (user, error) {
	c, e := r.Cookie("aside_session")
	if e != nil {
		return user{}, e
	}
	var u user
	e = a.db.QueryRow(r.Context(), `SELECT u.id::text,i.normalized_identifier FROM sessions s JOIN users u ON u.id=s.user_id JOIN user_identities i ON i.user_id=u.id AND i.type='email' AND i.verified_at IS NOT NULL WHERE s.token_hash=$1 AND s.guest_scope IS NULL AND s.revoked_at IS NULL AND s.expires_at>now() AND u.status='active' LIMIT 1`, digest(c.Value)).Scan(&u.ID, &u.Email)
	return u, e
}

// anySessionUser resolves a full or guest session to its verified user,
// without writing an error response.
func (a *API) anySessionUser(r *http.Request) (user, error) {
	c, e := r.Cookie("aside_session")
	if e != nil {
		return user{}, e
	}
	var u user
	e = a.db.QueryRow(r.Context(), `SELECT u.id::text,i.normalized_identifier FROM sessions s JOIN users u ON u.id=s.user_id JOIN user_identities i ON i.user_id=u.id AND i.type='email' AND i.verified_at IS NOT NULL WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND u.status='active' LIMIT 1`, digest(c.Value)).Scan(&u.ID, &u.Email)
	return u, e
}

func (a *API) requireUser(w http.ResponseWriter, r *http.Request) (user, bool) {
	u, e := a.currentUser(r)
	if e != nil {
		problem(w, 401, "AUTH_REQUIRED", "Sign in with a verified email to continue.")
		return user{}, false
	}
	return u, true
}

type guestSessionScope struct {
	Purpose    string   `json:"purpose"`
	QuoteIDs   []string `json:"quote_ids"`
	OfferIDs   []string `json:"offer_ids"`
	BookingIDs []string `json:"booking_ids"`
}

func (a *API) buyerActor(w http.ResponseWriter, r *http.Request) (user, bool, bool, guestSessionScope) {
	if u, err := a.currentUser(r); err == nil {
		return u, true, false, guestSessionScope{}
	}
	cookie, err := r.Cookie("aside_session")
	if err != nil {
		problem(w, 401, "AUTH_REQUIRED", "Verify your email to continue.")
		return user{}, false, false, guestSessionScope{}
	}
	var u user
	var scopeJSON []byte
	err = a.db.QueryRow(r.Context(), `SELECT u.id::text,i.normalized_identifier,s.guest_scope FROM sessions s JOIN users u ON u.id=s.user_id JOIN user_identities i ON i.user_id=u.id AND i.type='email' AND (i.verified_at IS NOT NULL OR s.guest_scope->>'unverified'='true') WHERE s.token_hash=$1 AND s.guest_scope IS NOT NULL AND s.revoked_at IS NULL AND s.expires_at>now() AND u.status='active' LIMIT 1`, digest(cookie.Value)).Scan(&u.ID, &u.Email, &scopeJSON)
	if err != nil {
		problem(w, 401, "AUTH_REQUIRED", "Verify your email to continue.")
		return user{}, false, false, guestSessionScope{}
	}
	var scope guestSessionScope
	if json.Unmarshal(scopeJSON, &scope) != nil {
		problem(w, 401, "GUEST_SCOPE_INVALID", "This restricted guest session is unavailable.")
		return user{}, false, false, guestSessionScope{}
	}
	return u, true, true, scope
}

func guestHas(scope guestSessionScope, field, id string) bool {
	values := scope.QuoteIDs
	if field == "offer_ids" {
		values = scope.OfferIDs
	} else if field == "booking_ids" {
		values = scope.BookingIDs
	}
	for _, value := range values {
		if value == id {
			return true
		}
	}
	return false
}

func (a *API) guestCanBooking(ctx context.Context, scope guestSessionScope, id string) bool {
	var matches bool
	if err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bookings WHERE id=$1 AND (id::text=ANY($2::text[]) OR quote_id::text=ANY($3::text[])))`, id, scope.BookingIDs, scope.QuoteIDs).Scan(&matches); err != nil {
		return false
	}
	return matches
}

func (a *API) guestCanReschedule(ctx context.Context, scope guestSessionScope, requestID string) bool {
	var bookingID string
	if err := a.db.QueryRow(ctx, `SELECT booking_id::text FROM reschedule_requests WHERE id=$1`, requestID).Scan(&bookingID); err != nil {
		return false
	}
	return a.guestCanBooking(ctx, scope, bookingID)
}

func addGuestResource(ctx context.Context, tx pgx.Tx, tokenHash []byte, field, id string) error {
	tag, err := tx.Exec(ctx, `UPDATE sessions SET guest_scope=jsonb_set(guest_scope,$2::text[],COALESCE(guest_scope #> $2::text[],'[]'::jsonb)||to_jsonb($3::text),true) WHERE token_hash=$1 AND guest_scope IS NOT NULL AND revoked_at IS NULL AND expires_at>now()`, tokenHash, []string{field}, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("guest session expired or revoked")
	}
	return nil
}

func requestSessionHash(r *http.Request) []byte {
	cookie, _ := r.Cookie("aside_session")
	return digest(cookie.Value)
}

func (a *API) mySessions(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	current := digestFromRequest(r)
	rows, err := a.db.Query(r.Context(), `SELECT token_hash,created_at,expires_at,mfa_verified_at FROM sessions WHERE user_id=$1 AND guest_scope IS NULL AND revoked_at IS NULL AND expires_at>now() ORDER BY created_at DESC`, u.ID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your active sessions could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var token []byte
		var created, expires time.Time
		var mfa *time.Time
		if err := rows.Scan(&token, &created, &expires, &mfa); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Active sessions could not be read.")
			return
		}
		items = append(items, map[string]any{"current": subtle.ConstantTimeCompare(token, current) == 1, "created_at": created, "expires_at": expires, "operations_verified": mfa != nil && mfa.After(time.Now().Add(-10*time.Hour))})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Active sessions could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"sessions": items})
}

func (a *API) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	tag, err := a.db.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND guest_scope IS NULL AND token_hash<>$2 AND revoked_at IS NULL AND expires_at>now()`, u.ID, digestFromRequest(r))
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Other sessions could not be revoked.")
		return
	}
	jsonOut(w, 200, map[string]int64{"revoked_sessions": tag.RowsAffected()})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var name, handle string
	var seller bool
	e := a.db.QueryRow(r.Context(), `SELECT u.display_name,COALESCE(sp.handle,''),sp.id IS NOT NULL FROM users u LEFT JOIN seller_profiles sp ON sp.user_id=u.id WHERE u.id=$1`, u.ID).Scan(&name, &handle, &seller)
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Account could not be loaded.")
		return
	}
	jsonOut(w, 200, map[string]any{"id": u.ID, "email": u.Email, "name": name, "handle": handle, "capabilities": map[string]bool{"seller": seller}})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	c, e := r.Cookie("aside_session")
	if e == nil {
		if _, err := a.db.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, digest(c.Value)); err != nil {
			problem(w, 503, "SESSION_ERROR", "Your session could not be revoked. Try again.")
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "aside_session", Value: "", Path: "/", HttpOnly: true, Secure: a.env == "production", SameSite: http.SameSiteLaxMode, MaxAge: -1})
	jsonOut(w, 200, map[string]bool{"logged_out": true})
}

func (a *API) createAccessChallenge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if decode(r, &in) != nil || !validEmail(in.Email) {
		problem(w, 422, "INVALID_EMAIL", "Enter a valid email address.")
		return
	}
	body, err := json.Marshal(challengeRequest{Email: in.Email, Purpose: "guest_access"})
	if err != nil {
		problem(w, 500, "CHALLENGE_ERROR", "A verification code could not be created.")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	a.createChallenge(w, r)
}
