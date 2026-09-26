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
	// NextHandleChange is when the seller may change their link again; empty
	// when they can change it now. Only the seller's own view carries it.
	NextHandleChange *time.Time `json:"next_handle_change_at,omitempty"`
}

// reserved are words used by the site itself (keep in step with apps/web/src/lib/handle.ts).
var reserved = map[string]bool{"api": true, "metrics": true, "status": true, "healthz": true, "app": true, "ops": true, "admin": true, "login": true, "claim": true, "help": true, "pricing": true, "terms": true, "privacy": true, "booking": true, "offer": true, "checkout": true, "payment": true, "auth": true, "r": true, "og": true, "access": true, "health": true, "dev": true, "settings": true, "support": true, "www": true, "assets": true, "book": true, "verify": true, "acceptable-use": true, "unsubscribe": true, "sitemap": true, "robots": true}

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
	// A seller's own earlier link is free for them to go back to.
	owner := ""
	if u, err := a.currentUser(r); err == nil {
		owner = u.ID
	}
	var taken bool
	e := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM seller_profiles WHERE handle=$1) OR EXISTS(SELECT 1 FROM handle_holds WHERE handle=$1 AND held_until>now()) OR EXISTS(SELECT 1 FROM handle_redirects hr JOIN seller_profiles sp ON sp.id=hr.seller_id WHERE hr.old_handle=$1 AND sp.user_id::text IS DISTINCT FROM NULLIF($2,''))`, h, owner).Scan(&taken)
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be checked.")
		return
	}
	jsonOut(w, 200, map[string]any{"handle": h, "available": !taken})
}

func (a *API) publicPerson(w http.ResponseWriter, r *http.Request) {
	h := normalizeHandle(r.PathValue("handle"))
	var p Person
	var sellerID string
	e := a.db.QueryRow(r.Context(), `SELECT sp.handle,u.display_name,COALESCE(sp.identity_url,''),sp.mode,COALESCE(pv.base_30_minor,0),COALESCE(pv.durations,ARRAY[15,30,60]),sp.paused,(sp.readiness_state='ready'),sp.timezone,sp.avatar_version,sp.public_version,sp.id::text,sp.country::text,sp.currency::text FROM seller_profiles sp JOIN users u ON u.id=sp.user_id LEFT JOIN LATERAL (SELECT base_30_minor,durations FROM pricing_versions WHERE seller_id=sp.id ORDER BY created_at DESC LIMIT 1) pv ON true WHERE sp.handle=$1 AND sp.publication_state='published'`, h).Scan(&p.Handle, &p.Name, &p.IdentityURL, &p.Mode, &p.Base30, &p.Durations, &p.Paused, &p.Ready, &p.Timezone, &p.AvatarVersion, &p.PublicVersion, &sellerID, &p.Country, &p.Currency)
	if errors.Is(e, pgx.ErrNoRows) {
		if moved := a.movedHandle(r, h); moved != "" {
			http.Redirect(w, r, "/api/v1/people/"+moved, http.StatusPermanentRedirect)
			return
		}
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
	}{p, cancellationRule, rating, paymentMethodsFor(p.Currency)})
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

// downsampleImage scales an image down so that max(width, height) <= maxDim.
func downsampleImage(src image.Image, maxDim int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim {
		return src
	}
	var newW, newH int
	if w >= h {
		newW = maxDim
		newH = int((int64(h)*int64(maxDim) + int64(w)/2) / int64(w))
	} else {
		newH = maxDim
		newW = int((int64(w)*int64(maxDim) + int64(h)/2) / int64(h))
	}
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		srcY := b.Min.Y + (y*h)/newH
		for x := 0; x < newW; x++ {
			srcX := b.Min.X + (x*w)/newW
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func (a *API) updateAvatar(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	// Multipart framing adds bytes beyond the file's advertised 10 MB limit.
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		problem(w, 413, "IMAGE_TOO_LARGE", "Choose an image smaller than 10 MB.")
		return
	}
	f, _, err := r.FormFile("avatar")
	if err != nil {
		problem(w, 400, "IMAGE_REQUIRED", "Choose a PNG or JPEG image.")
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (10<<20)+1))
	if err != nil || len(b) > 10<<20 {
		problem(w, 413, "IMAGE_TOO_LARGE", "Choose an image smaller than 10 MB.")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || (format != "png" && format != "jpeg") {
		problem(w, 422, "IMAGE_INVALID", "Use a valid PNG or JPEG image.")
		return
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 32000000 {
		problem(w, 422, "IMAGE_DIMENSIONS", "Image dimensions must be at most 8192 by 8192 pixels.")
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
	// Downsample to avatar dimensions (max 512x512) and re-encode to PNG to strip metadata.
	img = downsampleImage(img, 512)
	var clean bytes.Buffer
	if err = png.Encode(&clean, img); err != nil {
		problem(w, 422, "IMAGE_INVALID", "The image could not be processed.")
		return
	}
	// If still larger than 512 KB, downsample further until it fits under the database storage limit.
	for clean.Len() > 512<<10 && (img.Bounds().Dx() > 128 || img.Bounds().Dy() > 128) {
		clean.Reset()
		img = downsampleImage(img, img.Bounds().Dx()*3/4)
		if err = png.Encode(&clean, img); err != nil {
			break
		}
	}
	if clean.Len() > 512<<10 {
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
	// Every link has a price; "both" also takes offers.
	if p.Mode != "fixed" && p.Mode != "both" {
		return fmt.Errorf("choose a fixed price, with or without offers")
	}
	if p.Base30 < 100 || p.Base30 > 100000000 {
		return fmt.Errorf("enter a supported 30 minute price")
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
	if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM handle_holds WHERE handle=$1 AND held_until>now()) OR EXISTS(SELECT 1 FROM handle_redirects WHERE old_handle=$1)`, p.Handle).Scan(&held); e != nil {
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
	e := a.db.QueryRow(r.Context(), `SELECT sp.handle,u.display_name,COALESCE(sp.identity_url,''),sp.mode,COALESCE(pv.base_30_minor,0),COALESCE(pv.durations,ARRAY[15,30,60]),sp.paused,(sp.readiness_state='ready'),sp.timezone,sp.avatar_version,sp.public_version,sp.country::text,sp.currency::text,
		CASE WHEN sp.handle_changed_at+`+handleChangeWait+`>now() THEN sp.handle_changed_at+`+handleChangeWait+` END
		FROM seller_profiles sp JOIN users u ON u.id=sp.user_id LEFT JOIN LATERAL (SELECT base_30_minor,durations FROM pricing_versions WHERE seller_id=sp.id ORDER BY created_at DESC LIMIT 1) pv ON true WHERE sp.user_id=$1`, u.ID).Scan(&p.Handle, &p.Name, &p.IdentityURL, &p.Mode, &p.Base30, &p.Durations, &p.Paused, &p.Ready, &p.Timezone, &p.AvatarVersion, &p.PublicVersion, &p.Country, &p.Currency, &p.NextHandleChange)
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

// movedHandle returns the current link of a published seller who used to be
// at h, or "" when nobody was.
func (a *API) movedHandle(r *http.Request, h string) string {
	var current string
	if err := a.db.QueryRow(r.Context(), `SELECT sp.handle FROM handle_redirects hr JOIN seller_profiles sp ON sp.id=hr.seller_id WHERE hr.old_handle=$1 AND sp.publication_state='published'`, h).Scan(&current); err != nil {
		return ""
	}
	return current
}

// handleChangeWait is how long a seller waits between link changes, so shared
// links don't churn and nobody can sweep up names by cycling through them.
// The link chosen when signing up doesn't count as a change.
const handleChangeWait = `interval '6 months'`

// linkChangeWaitMessage tells a seller when they can change their link again.
func linkChangeWaitMessage(next time.Time, zone string) string {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	return "You can change your link once every 6 months. You can change it again on " + next.In(loc).Format("2 January 2006") + ", and we’ll email you then."
}

// changeHandle moves a seller to a new link. The old one keeps forwarding and
// stays theirs.
func (a *API) changeHandle(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var in struct {
		Handle string `json:"handle"`
	}
	if decode(r, &in) != nil {
		problem(w, 400, "INVALID_BODY", "Check the submitted details.")
		return
	}
	next := normalizeHandle(in.Handle)
	if !validHandle(next) {
		problem(w, 422, "INVALID_HANDLE", "Use 3 to 24 lowercase letters, numbers or single hyphens.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your link could not be changed.")
		return
	}
	defer tx.Rollback(r.Context())
	var sellerID, current, zone string
	var nextChange *time.Time
	err = tx.QueryRow(r.Context(), `SELECT id::text,handle,timezone,CASE WHEN handle_changed_at+`+handleChangeWait+`>now() THEN handle_changed_at+`+handleChangeWait+` END FROM seller_profiles WHERE user_id=$1 AND publication_state='published' FOR UPDATE`, u.ID).Scan(&sellerID, &current, &zone, &nextChange)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "PROFILE_NOT_FOUND", "Your link was not found.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your link could not be changed.")
		return
	}
	if next == current {
		jsonOut(w, 200, map[string]any{"handle": current})
		return
	}
	if nextChange != nil {
		problem(w, 429, "HANDLE_CHANGE_LIMIT", linkChangeWaitMessage(*nextChange, zone))
		return
	}
	var taken bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM seller_profiles WHERE handle=$1) OR EXISTS(SELECT 1 FROM handle_holds WHERE handle=$1 AND held_until>now()) OR EXISTS(SELECT 1 FROM handle_redirects WHERE old_handle=$1 AND seller_id<>$2)`, next, sellerID).Scan(&taken); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your link could not be changed.")
		return
	}
	if taken {
		problem(w, 409, "HANDLE_TAKEN", "That link is already taken. Try another.")
		return
	}
	// Going back to an earlier link of theirs: it stops being a forward.
	if _, err = tx.Exec(r.Context(), `DELETE FROM handle_redirects WHERE old_handle=$1 AND seller_id=$2`, next, sellerID); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your link could not be changed.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO handle_redirects(old_handle,seller_id) VALUES($1,$2) ON CONFLICT(old_handle) DO UPDATE SET seller_id=EXCLUDED.seller_id,created_at=now()`, current, sellerID); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your link could not be changed.")
		return
	}
	var canChangeAgain time.Time
	err = tx.QueryRow(r.Context(), `UPDATE seller_profiles SET handle=$2,public_version=public_version+1,handle_changed_at=now(),handle_reminder_due=now()+`+handleChangeWait+` WHERE id=$1 RETURNING handle_reminder_due`, sellerID, next).Scan(&canChangeAgain)
	if pgErrCode(err) == "23505" {
		problem(w, 409, "HANDLE_TAKEN", "That link is already taken. Try another.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your link could not be changed.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'seller.handle_changed',$2,'Seller changed their link',jsonb_build_object('from',$3::text,'to',$4::text))`, u.ID, sellerID, current, next); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your link could not be changed.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your link could not be changed.")
		return
	}
	jsonOut(w, 200, map[string]any{"handle": next, "previous": current, "next_handle_change_at": canChangeAgain})
}
