package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type Person struct {
	Handle        string `json:"handle"`
	Name          string `json:"name"`
	IdentityURL   string `json:"identity_url,omitempty"`
	IdentityLabel string `json:"identity_label,omitempty"`
	AvatarVersion int64  `json:"avatar_version,omitempty"`
	PublicVersion int64  `json:"public_version,omitempty"`
	Mode          string `json:"mode"`
	Base30        int64  `json:"base_30_minor"`
	Durations     []int  `json:"durations"`
	Paused        bool   `json:"paused"`
	Ready         bool   `json:"ready"`
	Timezone      string `json:"timezone"`
	// Country is where the seller is based and paid; it fixes the currency.
	// It is chosen when the link is claimed and is not changed by an edit.
	Country          string `json:"country,omitempty"`
	Currency         string `json:"currency,omitempty"`
	LocalSimulator   bool   `json:"local_simulator,omitempty"`
	ProviderCheckout bool   `json:"provider_checkout_enabled,omitempty"`
}

var reserved = map[string]bool{"api": true, "metrics": true, "status": true, "healthz": true, "app": true, "ops": true, "admin": true, "login": true, "claim": true, "help": true, "pricing": true, "terms": true, "privacy": true, "booking": true, "offer": true, "checkout": true, "payment": true, "auth": true, "r": true, "og": true, "access": true, "health": true, "dev": true, "settings": true, "support": true, "www": true, "assets": true}

func normalizeHandle(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

func validHandle(v string) bool {
	if len(v) < 3 || len(v) > 24 || reserved[v] || v[0] == '-' || v[len(v)-1] == '-' || strings.Contains(v, "--") {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// validName accepts a trimmed display name of 1-80 bytes with no control
// characters (a line break would break email headers and receipts).
func validName(v string) bool {
	if v == "" || len(v) > 80 || !utf8.ValidString(v) {
		return false
	}
	for _, c := range v {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
			return false
		}
	}
	return true
}

func (a *API) availability(w http.ResponseWriter, r *http.Request) {
	h := normalizeHandle(r.PathValue("handle"))
	if !validHandle(h) {
		jsonOut(w, 200, map[string]any{"handle": h, "available": false})
		return
	}
	var taken bool
	e := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM seller_profiles WHERE handle=$1) OR EXISTS(SELECT 1 FROM handle_holds WHERE handle=$1 AND held_until>now())`, h).Scan(&taken)
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be checked.")
		return
	}
	jsonOut(w, 200, map[string]any{"handle": h, "available": !taken})
}

func (a *API) publicPerson(w http.ResponseWriter, r *http.Request) {
	h := normalizeHandle(r.PathValue("handle"))
	var p Person
	var sellerID, policy string
	e := a.db.QueryRow(r.Context(), `SELECT sp.handle,u.display_name,COALESCE(sp.identity_url,''),sp.mode,COALESCE(pv.base_30_minor,0),COALESCE(pv.durations,ARRAY[15,30,60]),sp.paused,(sp.readiness_state='ready'),sp.timezone,sp.avatar_version,sp.public_version,sp.id::text,sp.cancellation_policy,sp.country::text,sp.currency::text FROM seller_profiles sp JOIN users u ON u.id=sp.user_id LEFT JOIN LATERAL (SELECT base_30_minor,durations FROM pricing_versions WHERE seller_id=sp.id ORDER BY created_at DESC LIMIT 1) pv ON true WHERE sp.handle=$1 AND sp.publication_state='published'`, h).Scan(&p.Handle, &p.Name, &p.IdentityURL, &p.Mode, &p.Base30, &p.Durations, &p.Paused, &p.Ready, &p.Timezone, &p.AvatarVersion, &p.PublicVersion, &sellerID, &policy, &p.Country, &p.Currency)
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This link is not available.")
		return
	}
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "This link could not be loaded.")
		return
	}
	if p.IdentityURL != "" {
		if parsed, err := url.Parse(p.IdentityURL); err == nil {
			p.IdentityLabel = identityLinkLabel(parsed.Hostname())
		}
	}
	p.LocalSimulator = a.localPaymentSimulatorEnabled()
	p.ProviderCheckout = a.checkoutReadyFor(p.Currency)
	rating, err := a.sellerReviewSummary(r.Context(), sellerID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "This link could not be loaded.")
		return
	}
	// Output-only fields are added here so the Person input type never accepts them.
	jsonOut(w, 200, struct {
		Person
		CancellationPolicy cancellationPolicy `json:"cancellation_policy"`
		Rating             reviewSummary      `json:"rating"`
		PaymentMethods     []string           `json:"payment_methods"`
	}{p, policyOrDefault(policy), rating, paymentMethodsFor(p.Currency)})
}

func (a *API) publicAvatar(w http.ResponseWriter, r *http.Request) {
	h := normalizeHandle(r.PathValue("handle"))
	version, versionErr := strconv.ParseInt(r.URL.Query().Get("v"), 10, 64)
	if !validHandle(h) || versionErr != nil || version < 1 {
		http.NotFound(w, r)
		return
	}
	var mime, key string
	var data []byte
	err := a.db.QueryRow(r.Context(), `SELECT sp.avatar_mime,sp.avatar_data,COALESCE(sp.avatar_key,'') FROM seller_profiles sp WHERE sp.handle=$1 AND sp.publication_state='published' AND sp.avatar_mime IS NOT NULL AND sp.avatar_version=$2`, h, version).Scan(&mime, &data, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The profile image could not be loaded.")
		return
	}
	if key != "" {
		if a.media == nil {
			problem(w, 503, "MEDIA_UNAVAILABLE", "The profile image could not be loaded.")
			return
		}
		// A public bucket domain serves the file straight from Cloudflare's edge.
		if public := a.media.publicURL(key); public != "" {
			w.Header().Set("Cache-Control", "public, max-age=300")
			http.Redirect(w, r, public, http.StatusFound)
			return
		}
		data, _, err = a.media.get(r.Context(), key, 512<<10)
		if errors.Is(err, errObjectNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			a.log().WarnContext(r.Context(), "profile image fetch failed", "error", err.Error())
			problem(w, 503, "MEDIA_UNAVAILABLE", "The profile image could not be loaded.")
			return
		}
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=300, stale-while-revalidate=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *API) updateAvatar(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	// Multipart framing adds bytes beyond the file's advertised 2 MB limit.
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		problem(w, 413, "IMAGE_TOO_LARGE", "Choose an image smaller than 2 MB.")
		return
	}
	f, _, err := r.FormFile("avatar")
	if err != nil {
		problem(w, 400, "IMAGE_REQUIRED", "Choose a PNG or JPEG image.")
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil || len(b) > 2<<20 {
		problem(w, 413, "IMAGE_TOO_LARGE", "Choose an image smaller than 2 MB.")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || (format != "png" && format != "jpeg") {
		problem(w, 422, "IMAGE_INVALID", "Use a valid PNG or JPEG image.")
		return
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 12000000 {
		problem(w, 422, "IMAGE_DIMENSIONS", "Image dimensions must be at most 4096 by 4096 pixels.")
		return
	}
	img, decodedFormat, err := image.Decode(bytes.NewReader(b))
	if err != nil || decodedFormat != format {
		problem(w, 422, "IMAGE_INVALID", "Use a valid PNG or JPEG image.")
		return
	}
	bounds := img.Bounds()
	if bounds.Dx() != config.Width || bounds.Dy() != config.Height {
		problem(w, 422, "IMAGE_INVALID", "The image could not be decoded safely.")
		return
	}
	var sid, handle string
	err = a.db.QueryRow(r.Context(), `SELECT id::text,handle FROM seller_profiles WHERE user_id=$1`, u.ID).Scan(&sid, &handle)
	if err != nil {
		problem(w, 404, "PROFILE_NOT_FOUND", "Claim a link before adding a profile image.")
		return
	}
	// Decode and re-encode pixels so source metadata is not retained.
	var clean bytes.Buffer
	if err = png.Encode(&clean, img); err != nil || clean.Len() > 512<<10 {
		problem(w, 422, "IMAGE_TOO_LARGE", "The processed image is too large. Choose a smaller image.")
		return
	}
	const mime = "image/png"
	var avatarVersion int64
	var oldKey string
	if err = a.db.QueryRow(r.Context(), `SELECT COALESCE(avatar_key,'') FROM seller_profiles WHERE id=$1`, sid).Scan(&oldKey); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The profile image could not be saved.")
		return
	}
	if a.media != nil {
		// Each upload gets a new unguessable name, so the file can be cached forever.
		key := "avatars/" + sid + "/" + randomHex(16) + ".png"
		if err = a.media.put(r.Context(), key, mime, "public, max-age=31536000, immutable", clean.Bytes()); err != nil {
			a.log().WarnContext(r.Context(), "profile image upload failed", "error", err.Error())
			problem(w, 503, "MEDIA_UNAVAILABLE", "The profile image could not be saved. Try again shortly.")
			return
		}
		err = a.db.QueryRow(r.Context(), `UPDATE seller_profiles SET avatar_mime=$2,avatar_data=NULL,avatar_key=$3,avatar_version=avatar_version+1,public_version=public_version+1 WHERE id=$1 RETURNING avatar_version`, sid, mime, key).Scan(&avatarVersion)
		if err != nil {
			a.removeMedia(key)
		}
	} else {
		err = a.db.QueryRow(r.Context(), `UPDATE seller_profiles SET avatar_mime=$2,avatar_data=$3,avatar_key=NULL,avatar_version=avatar_version+1,public_version=public_version+1 WHERE id=$1 RETURNING avatar_version`, sid, mime, clean.Bytes()).Scan(&avatarVersion)
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The profile image could not be saved.")
		return
	}
	a.removeMedia(oldKey)
	jsonOut(w, 200, map[string]any{"url": "/api/v1/people/" + url.PathEscape(handle) + "/avatar", "mime_type": mime, "avatar_version": avatarVersion})
}

// removeMedia deletes a replaced file in the background. A failure only
// leaves an orphan that nothing links to.
func (a *API) removeMedia(key string) {
	if key == "" || a.media == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.media.delete(ctx, key); err != nil {
			a.log().Warn("old profile image not removed", "key", key, "error", err.Error())
		}
	}()
}

func (a *API) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var oldKey string
	err := a.db.QueryRow(r.Context(), `UPDATE seller_profiles sp SET avatar_mime=NULL,avatar_data=NULL,avatar_key=NULL,avatar_version=avatar_version+1,public_version=public_version+1 FROM (SELECT id,avatar_key FROM seller_profiles WHERE user_id=$1 FOR UPDATE) old WHERE sp.id=old.id RETURNING COALESCE(old.avatar_key,'')`, u.ID).Scan(&oldKey)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "The profile image could not be removed.")
		return
	}
	a.removeMedia(oldKey)
	w.WriteHeader(http.StatusNoContent)
}

func validatePerson(p *Person) error {
	p.Handle = normalizeHandle(p.Handle)
	p.Name = strings.TrimSpace(p.Name)
	if !validHandle(p.Handle) || !validName(p.Name) {
		return fmt.Errorf("choose a valid link and display name")
	}
	if p.Mode != "fixed" && p.Mode != "offer" {
		return fmt.Errorf("choose fixed price or offers")
	}
	if p.Mode == "fixed" && (p.Base30 < 100 || p.Base30 > 100000000) {
		return fmt.Errorf("enter a supported 30 minute price")
	}
	if p.Mode == "offer" {
		p.Base30 = 0
	}
	if len(p.Durations) == 0 {
		p.Durations = []int{15, 30, 60}
	}
	for _, d := range p.Durations {
		if d != 15 && d != 30 && d != 60 {
			return fmt.Errorf("durations must be 15, 30 or 60 minutes")
		}
	}
	if p.Timezone == "" {
		p.Timezone = "UTC"
	}
	if _, e := loadNamedTimezone(p.Timezone); e != nil {
		return fmt.Errorf("choose a valid timezone")
	}
	p.IdentityURL = strings.TrimSpace(p.IdentityURL)
	p.IdentityLabel = ""
	if p.IdentityURL != "" {
		parsed, err := url.ParseRequestURI(p.IdentityURL)
		if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(p.IdentityURL) > 512 {
			return fmt.Errorf("enter a secure link to your social profile")
		}
		host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
		label := identityLinkLabel(host)
		allowed := label != ""
		if !allowed || parsed.Port() != "" || len(parsed.Path) < 2 {
			return fmt.Errorf("use an Instagram, LinkedIn, X, TikTok, YouTube or GitHub profile link")
		}
		p.IdentityURL = "https://" + strings.ToLower(parsed.Host) + parsed.EscapedPath()
		p.IdentityLabel = label
	}
	p.Ready = false
	return nil
}

func identityLinkLabel(host string) string {
	labels := map[string]string{"instagram.com": "Instagram", "linkedin.com": "LinkedIn", "x.com": "X", "twitter.com": "X", "tiktok.com": "TikTok", "youtube.com": "YouTube", "github.com": "GitHub"}
	return labels[strings.ToLower(strings.TrimPrefix(host, "www."))]
}

func (a *API) claimProfile(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var p Person
	if decode(r, &p) != nil {
		problem(w, 400, "INVALID_BODY", "Check the submitted details.")
		return
	}
	if e := validatePerson(&p); e != nil {
		problem(w, 422, "INVALID_PROFILE", e.Error())
		return
	}
	if p.Country == "" {
		p.Country = guessCountry(p.Timezone)
	}
	seat, known := marketFor(p.Country)
	if !known {
		problem(w, 422, "COUNTRY_NOT_SUPPORTED", "WantMyTime can’t pay sellers in that country yet.")
		return
	}
	p.Country, p.Currency = seat.Country, seat.Currency
	tx, e := a.db.Begin(r.Context())
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Profile could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), `SELECT id FROM users WHERE id=$1 FOR UPDATE`, u.ID); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Profile could not be saved.")
		return
	}
	var existingHandle string
	if e = tx.QueryRow(r.Context(), `SELECT handle FROM seller_profiles WHERE user_id=$1`, u.ID).Scan(&existingHandle); e == nil {
		problem(w, 409, "PROFILE_EXISTS", "This account already has the link /"+existingHandle+". Edit it from your workspace.")
		return
	} else if !errors.Is(e, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "Profile could not be saved.")
		return
	}
	var held bool
	if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM handle_holds WHERE handle=$1 AND held_until>now())`, p.Handle).Scan(&held); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Profile could not be saved.")
		return
	}
	if held {
		problem(w, 409, "HANDLE_TAKEN", "That link is already claimed.")
		return
	}
	if _, e = tx.Exec(r.Context(), `UPDATE users SET display_name=$2 WHERE id=$1`, u.ID, p.Name); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Profile could not be saved.")
		return
	}
	var sid string
	e = tx.QueryRow(r.Context(), `INSERT INTO seller_profiles(id,user_id,handle,mode,publication_state,readiness_state,timezone,identity_url,country,currency) VALUES(gen_random_uuid(),$1,$2,$3,'published',CASE WHEN $6 THEN 'ready' ELSE 'incomplete' END,$4,NULLIF($5,''),$7,$8) RETURNING id::text`, u.ID, p.Handle, p.Mode, p.Timezone, p.IdentityURL, a.localPaymentSimulatorEnabled(), p.Country, p.Currency).Scan(&sid)
	if pgErrCode(e) == "23505" {
		problem(w, 409, "HANDLE_TAKEN", "That link is already claimed.")
		return
	}
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Profile could not be saved.")
		return
	}
	_, e = tx.Exec(r.Context(), `INSERT INTO pricing_versions(id,seller_id,currency,base_30_minor,durations,fee_basis_points) VALUES(gen_random_uuid(),$1,$4,$2,$3,500)`, sid, p.Base30, p.Durations, p.Currency)
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Profile pricing could not be saved.")
		return
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO product_events(id,event_name,subject_hash,environment,props,occurred_at,seller_id) VALUES(gen_random_uuid(),'seller_profile_published',$1,$2,'{}',now(),$3)`, analyticsSubjectHash(u.ID), a.env, sid); e != nil {
		problem(w, 503, "EVENT_UNAVAILABLE", "Profile activation could not be recorded.")
		return
	}
	// Attribute only a verified production buyer who deliberately clicked the
	// seller CTA from their paid booking receipt and then claimed a first link
	// within the following 30 days. Local/sandbox activity and self-payments are
	// excluded by construction.
	if a.env == "production" {
		var qualifyingBookingID, sourceUserID string
		attributionErr := tx.QueryRow(r.Context(), `SELECT b.id::text,source.user_id::text
			FROM bookings b
			JOIN seller_profiles source ON source.id=b.seller_id
			JOIN users source_user ON source_user.id=source.user_id AND source_user.status='active'
			JOIN payment_attempts pa ON pa.booking_id=b.id AND pa.canonical_state='success' AND pa.environment='live' AND pa.last_verified_at IS NOT NULL
			WHERE b.buyer_user_id=$1 AND b.payment_state='paid' AND source.user_id<>$1
			AND pa.last_verified_at>=now()-interval '30 days'
			AND EXISTS (SELECT 1 FROM product_events c WHERE c.event_name='seller_cta_clicked' AND c.seller_id=source.id AND c.subject_hash=$2 AND c.props->>'booking_id'=b.id::text AND c.occurred_at>=pa.last_verified_at AND c.occurred_at<=pa.last_verified_at+interval '30 days')
			ORDER BY pa.last_verified_at,b.id LIMIT 1`, u.ID, analyticsSubjectHash(u.ID)).Scan(&qualifyingBookingID, &sourceUserID)
		if attributionErr == nil {
			if _, e = tx.Exec(r.Context(), `INSERT INTO growth_relationships(id,source_user_id,child_user_id,qualifying_booking_id,attribution_method) VALUES(gen_random_uuid(),$1,$2,$3,'verified_paid_booking_and_receipt_cta') ON CONFLICT DO NOTHING`, sourceUserID, u.ID, qualifyingBookingID); e != nil {
				problem(w, 503, "ATTRIBUTION_UNAVAILABLE", "Your link was not saved because its eligible attribution could not be recorded.")
				return
			}
		} else if !errors.Is(attributionErr, pgx.ErrNoRows) {
			problem(w, 503, "ATTRIBUTION_UNAVAILABLE", "Your link could not be saved.")
			return
		}
	}
	if e = tx.Commit(r.Context()); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Profile could not be saved.")
		return
	}
	jsonOut(w, 201, p)
}

func (a *API) getOwnProfile(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var p Person
	e := a.db.QueryRow(r.Context(), `SELECT sp.handle,u.display_name,COALESCE(sp.identity_url,''),sp.mode,COALESCE(pv.base_30_minor,0),COALESCE(pv.durations,ARRAY[15,30,60]),sp.paused,(sp.readiness_state='ready'),sp.timezone,sp.avatar_version,sp.public_version,sp.country::text,sp.currency::text FROM seller_profiles sp JOIN users u ON u.id=sp.user_id LEFT JOIN LATERAL (SELECT base_30_minor,durations FROM pricing_versions WHERE seller_id=sp.id ORDER BY created_at DESC LIMIT 1) pv ON true WHERE sp.user_id=$1`, u.ID).Scan(&p.Handle, &p.Name, &p.IdentityURL, &p.Mode, &p.Base30, &p.Durations, &p.Paused, &p.Ready, &p.Timezone, &p.AvatarVersion, &p.PublicVersion, &p.Country, &p.Currency)
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "PROFILE_NOT_FOUND", "You have not claimed a link yet.")
		return
	}
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Your link could not be loaded.")
		return
	}
	if p.IdentityURL != "" {
		if parsed, err := url.Parse(p.IdentityURL); err == nil {
			p.IdentityLabel = identityLinkLabel(parsed.Hostname())
		}
	}
	jsonOut(w, 200, p)
}

func (a *API) updateProfile(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var p Person
	if decode(r, &p) != nil {
		problem(w, 400, "INVALID_BODY", "Check the submitted details.")
		return
	}
	if e := validatePerson(&p); e != nil {
		problem(w, 422, "INVALID_PROFILE", e.Error())
		return
	}
	tx, e := a.db.Begin(r.Context())
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Profile could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), `UPDATE users SET display_name=$2 WHERE id=$1`, u.ID, p.Name); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Profile could not be saved.")
		return
	}
	var sid string
	e = tx.QueryRow(r.Context(), `UPDATE seller_profiles SET mode=$2,timezone=$3,paused=$4,identity_url=NULLIF($5,''),public_version=public_version+1 WHERE user_id=$1 AND handle=$6 RETURNING id::text,country::text,currency::text`, u.ID, p.Mode, p.Timezone, p.Paused, p.IdentityURL, p.Handle).Scan(&sid, &p.Country, &p.Currency)
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "PROFILE_NOT_FOUND", "Your link was not found.")
		return
	}
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Profile could not be saved.")
		return
	}
	if _, e = tx.Exec(r.Context(), `UPDATE availability_windows SET timezone=$2 WHERE seller_id=$1`, sid, p.Timezone); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability timezone could not be updated.")
		return
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO pricing_versions(id,seller_id,currency,base_30_minor,durations,fee_basis_points) VALUES(gen_random_uuid(),$1,$4,$2,$3,500)`, sid, p.Base30, p.Durations, p.Currency); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Pricing could not be saved.")
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Profile could not be saved.")
		return
	}
	jsonOut(w, 200, p)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
