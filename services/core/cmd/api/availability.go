package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	_ "image/jpeg"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

type availabilityWindow struct {
	Weekday int    `json:"weekday"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

type availabilityConfig struct {
	Timezone      string                 `json:"timezone"`
	MinimumNotice int                    `json:"minimum_notice_minutes"`
	HorizonDays   int                    `json:"booking_horizon_days"`
	BufferMinutes int                    `json:"buffer_minutes"`
	Windows       []availabilityWindow   `json:"windows"`
	Overrides     []availabilityOverride `json:"overrides"`
}

type availabilityOverride struct {
	Date    string               `json:"date"`
	Closed  bool                 `json:"closed"`
	Windows []availabilityWindow `json:"windows,omitempty"`
}

type slotResult struct {
	StartsAt time.Time `json:"starts_at"`
	// Local is labelled in the viewer's zone (the seller's when none was given).
	Local          string `json:"local_label"`
	Timezone       string `json:"timezone"`
	SellerLabel    string `json:"seller_label"`
	ViewerTimezone string `json:"viewer_timezone"`
}

func (a *API) getAvailability(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var out availabilityConfig
	var sellerID string
	e := a.db.QueryRow(r.Context(), `SELECT id::text,timezone,minimum_notice_minutes,booking_horizon_days,buffer_minutes FROM seller_profiles WHERE user_id=$1`, u.ID).Scan(&sellerID, &out.Timezone, &out.MinimumNotice, &out.HorizonDays, &out.BufferMinutes)
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "PROFILE_NOT_FOUND", "Claim your link before setting availability.")
		return
	}
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be loaded.")
		return
	}
	rows, e := a.db.Query(r.Context(), `SELECT weekday,to_char(local_start,'HH24:MI'),to_char(local_end,'HH24:MI') FROM availability_windows WHERE seller_id=$1 ORDER BY weekday,local_start`, sellerID)
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be loaded.")
		return
	}
	defer rows.Close()
	out.Windows = []availabilityWindow{}
	for rows.Next() {
		var window availabilityWindow
		if e = rows.Scan(&window.Weekday, &window.Start, &window.End); e != nil {
			problem(w, 503, "DATABASE_ERROR", "Availability could not be loaded.")
			return
		}
		out.Windows = append(out.Windows, window)
	}
	if e = rows.Err(); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be loaded.")
		return
	}
	rows, e = a.db.Query(r.Context(), `SELECT local_date::text,closed,replacement_windows FROM availability_overrides WHERE seller_id=$1 AND local_date>=CURRENT_DATE ORDER BY local_date LIMIT 120`, sellerID)
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be loaded.")
		return
	}
	defer rows.Close()
	out.Overrides = []availabilityOverride{}
	for rows.Next() {
		var override availabilityOverride
		var replacement []byte
		if e = rows.Scan(&override.Date, &override.Closed, &replacement); e != nil {
			problem(w, 503, "DATABASE_ERROR", "Availability could not be loaded.")
			return
		}
		if len(replacement) > 0 && string(replacement) != "null" {
			if e = json.Unmarshal(replacement, &override.Windows); e != nil {
				problem(w, 503, "DATABASE_ERROR", "Availability could not be loaded.")
				return
			}
		}
		out.Overrides = append(out.Overrides, override)
	}
	if e = rows.Err(); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be loaded.")
		return
	}
	jsonOut(w, 200, out)
}

func (a *API) putAvailability(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var in availabilityConfig
	if decode(r, &in) != nil || len(in.Windows) > 56 || in.MinimumNotice < 0 || in.MinimumNotice > 10080 || in.HorizonDays < 1 || in.HorizonDays > 365 || in.BufferMinutes < 0 || in.BufferMinutes > 240 {
		problem(w, 422, "INVALID_AVAILABILITY", "Check notice, horizon, buffer and weekly hours.")
		return
	}
	in.Timezone = strings.TrimSpace(in.Timezone)
	location, e := loadNamedTimezone(in.Timezone)
	if e != nil {
		problem(w, 422, "INVALID_TIMEZONE", "Choose a valid timezone.")
		return
	}
	type minuteWindow struct{ start, end int }
	perDay := map[int][]minuteWindow{}
	for i, window := range in.Windows {
		if window.Weekday < 0 || window.Weekday > 6 {
			problem(w, 422, "INVALID_AVAILABILITY", "Choose a valid weekday.")
			return
		}
		sm, ok1 := parseClockMinutes(window.Start)
		em, ok2 := parseClockMinutes(window.End)
		if !ok1 || !ok2 || sm >= em || sm%15 != 0 || em%15 != 0 {
			problem(w, 422, "INVALID_AVAILABILITY", "Hours must use 15 minute increments and end after they start.")
			return
		}
		window.Start, window.End = formatClockMinutes(sm), formatClockMinutes(em)
		in.Windows[i] = window
		perDay[window.Weekday] = append(perDay[window.Weekday], minuteWindow{sm, em})
	}
	for _, windows := range perDay {
		sort.Slice(windows, func(i, j int) bool { return windows[i].start < windows[j].start })
		for i := 1; i < len(windows); i++ {
			if windows[i].start < windows[i-1].end {
				problem(w, 422, "OVERLAPPING_WINDOWS", "Weekly hours cannot overlap.")
				return
			}
		}
	}
	tx, e := a.db.Begin(r.Context())
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	var sellerID string
	e = tx.QueryRow(r.Context(), `UPDATE seller_profiles SET timezone=$2,minimum_notice_minutes=$3,booking_horizon_days=$4,buffer_minutes=$5 WHERE user_id=$1 RETURNING id::text`, u.ID, location.String(), in.MinimumNotice, in.HorizonDays, in.BufferMinutes).Scan(&sellerID)
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "PROFILE_NOT_FOUND", "Claim your link before setting availability.")
		return
	}
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be saved.")
		return
	}
	if _, e = tx.Exec(r.Context(), `DELETE FROM availability_windows WHERE seller_id=$1`, sellerID); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be saved.")
		return
	}
	for _, window := range in.Windows {
		if _, e = tx.Exec(r.Context(), `INSERT INTO availability_windows(id,seller_id,weekday,local_start,local_end,timezone) VALUES(gen_random_uuid(),$1,$2,$3::time,$4::time,$5)`, sellerID, window.Weekday, window.Start, window.End, location.String()); e != nil {
			problem(w, 503, "DATABASE_ERROR", "Availability could not be saved.")
			return
		}
	}
	if e = tx.Commit(r.Context()); e != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be saved.")
		return
	}
	jsonOut(w, 200, in)
}

func (a *API) putAvailabilityOverride(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	date, err := time.Parse("2006-01-02", r.PathValue("date"))
	if err != nil {
		problem(w, 422, "INVALID_DATE", "Choose a valid date.")
		return
	}
	var in availabilityOverride
	if decode(r, &in) != nil || len(in.Windows) > 12 || (in.Closed && len(in.Windows) > 0) || (!in.Closed && len(in.Windows) == 0) {
		problem(w, 422, "INVALID_OVERRIDE", "Choose a closed date or replacement hours.")
		return
	}
	var sellerID, zone string
	var horizon int
	err = a.db.QueryRow(r.Context(), `SELECT id::text,timezone,booking_horizon_days FROM seller_profiles WHERE user_id=$1`, u.ID).Scan(&sellerID, &zone, &horizon)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "PROFILE_NOT_FOUND", "Claim your link before setting date overrides.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Date override could not be saved.")
		return
	}
	today := time.Now().In(mustLocation(zone))
	date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, today.Location())
	if date.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())) || date.After(today.AddDate(0, 0, horizon)) {
		problem(w, 422, "DATE_OUT_OF_RANGE", "Choose a date within your booking horizon.")
		return
	}
	perDay := []intWindow{}
	for i, window := range in.Windows {
		sm, ok1 := parseClockMinutes(window.Start)
		em, ok2 := parseClockMinutes(window.End)
		if !ok1 || !ok2 || sm >= em || sm%15 != 0 || em%15 != 0 {
			problem(w, 422, "INVALID_OVERRIDE", "Replacement hours must use 15 minute increments and end after they start.")
			return
		}
		in.Windows[i].Start, in.Windows[i].End = formatClockMinutes(sm), formatClockMinutes(em)
		perDay = append(perDay, intWindow{start: sm, end: em})
	}
	sort.Slice(perDay, func(i, j int) bool { return perDay[i].start < perDay[j].start })
	for i := 1; i < len(perDay); i++ {
		if perDay[i].start < perDay[i-1].end {
			problem(w, 422, "OVERLAPPING_WINDOWS", "Replacement hours cannot overlap.")
			return
		}
	}
	in.Date = date.Format("2006-01-02")
	var payload any
	if !in.Closed {
		payload, _ = json.Marshal(in.Windows)
	}
	_, err = a.db.Exec(r.Context(), `INSERT INTO availability_overrides(id,seller_id,local_date,closed,replacement_windows) VALUES(gen_random_uuid(),$1,$2,$3,$4) ON CONFLICT(seller_id,local_date) DO UPDATE SET closed=EXCLUDED.closed,replacement_windows=EXCLUDED.replacement_windows`, sellerID, in.Date, in.Closed, payload)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Date override could not be saved.")
		return
	}
	jsonOut(w, 200, in)
}

type intWindow struct{ start, end int }

// parseClockMinutes accepts "HH:MM" or "HH:MM:SS" (seconds must be zero) and
// returns minutes after midnight.
func parseClockMinutes(value string) (int, bool) {
	value = strings.TrimSpace(value)
	layout := "15:04"
	if len(value) == len("15:04:05") {
		layout = "15:04:05"
	}
	t, err := time.Parse(layout, value)
	if err != nil || t.Second() != 0 {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func formatClockMinutes(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// loadNamedTimezone accepts only named IANA zones; "" and "Local" would
// silently depend on the server's configuration.
func loadNamedTimezone(zone string) (*time.Location, error) {
	if zone == "" || zone == "Local" || len(zone) > 64 {
		return nil, errors.New("invalid timezone")
	}
	return time.LoadLocation(zone)
}

func mustLocation(zone string) *time.Location {
	location, err := time.LoadLocation(zone)
	if err != nil {
		return time.UTC
	}
	return location
}

func (a *API) deleteAvailabilityOverride(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	date, err := time.Parse("2006-01-02", r.PathValue("date"))
	if err != nil {
		problem(w, 422, "INVALID_DATE", "Choose a valid date.")
		return
	}
	_, err = a.db.Exec(r.Context(), `DELETE FROM availability_overrides o USING seller_profiles sp WHERE sp.user_id=$1 AND o.seller_id=sp.id AND o.local_date=$2`, u.ID, date.Format("2006-01-02"))
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Date override could not be removed.")
		return
	}
	jsonOut(w, 200, map[string]bool{"removed": true})
}

// publicSlots lists bookable start times for one calendar date. The date is
// read in the viewer's zone when a valid ?tz= is given (so a buyer abroad sees
// their own day, which may span two of the seller's days), otherwise in the
// seller's zone. Every slot carries labels for both zones.
func (a *API) publicSlots(w http.ResponseWriter, r *http.Request) {
	handle := normalizeHandle(r.PathValue("handle"))
	date, err := time.Parse("2006-01-02", r.URL.Query().Get("date"))
	duration, durationErr := strconv.Atoi(r.URL.Query().Get("duration"))
	if !validHandle(handle) || err != nil || durationErr != nil || (duration != 15 && duration != 30 && duration != 60) {
		problem(w, 422, "INVALID_SLOT_QUERY", "Choose a valid date and conversation length.")
		return
	}
	var seller slotSeller
	var paused, ready bool
	err = a.db.QueryRow(r.Context(), `SELECT id::text,timezone,paused,(readiness_state='ready'),minimum_notice_minutes,booking_horizon_days,buffer_minutes FROM seller_profiles WHERE handle=$1 AND publication_state='published'`, handle).Scan(&seller.id, &seller.zone, &paused, &ready, &seller.notice, &seller.horizon, &seller.buffer)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This link is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Availability could not be loaded.")
		return
	}
	if seller.location, err = time.LoadLocation(seller.zone); err != nil {
		problem(w, 503, "INVALID_SELLER_TIMEZONE", "Availability is not configured correctly.")
		return
	}
	viewer := seller.location
	if tz := strings.TrimSpace(r.URL.Query().Get("tz")); tz != "" {
		if viewer, err = loadNamedTimezone(tz); err != nil {
			problem(w, 422, "INVALID_TIMEZONE", "Choose a valid timezone.")
			return
		}
	}
	empty := func() {
		jsonOut(w, 200, map[string]any{"slots": []slotResult{}, "timezone": seller.zone, "viewer_timezone": viewer.String()})
	}
	if paused || !ready {
		empty()
		return
	}
	// The viewer's calendar day as an instant range, and the seller dates it touches.
	from := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, viewer)
	to := time.Date(date.Year(), date.Month(), date.Day()+1, 0, 0, 0, 0, viewer)
	first := from.In(seller.location)
	last := to.Add(-time.Nanosecond).In(seller.location)
	result := []slotResult{}
	for day := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, seller.location); !day.After(last); day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, seller.location) {
		starts, dayErr := a.sellerDaySlots(r.Context(), seller, day, duration)
		if dayErr != nil {
			problem(w, 503, "DATABASE_ERROR", "Availability could not be loaded.")
			return
		}
		for _, start := range starts {
			if start.Before(from) || !start.Before(to) {
				continue
			}
			result = append(result, slotResult{
				StartsAt:       start.UTC(),
				Local:          start.In(viewer).Format("Mon, Jan 2 · 3:04 PM MST"),
				Timezone:       seller.zone,
				SellerLabel:    start.In(seller.location).Format("Mon, Jan 2 · 3:04 PM MST"),
				ViewerTimezone: viewer.String(),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartsAt.Before(result[j].StartsAt) })
	jsonOut(w, 200, map[string]any{"slots": result, "timezone": seller.zone, "viewer_timezone": viewer.String()})
}

type slotSeller struct {
	id, zone                string
	location                *time.Location
	notice, horizon, buffer int
}

// sellerDaySlots returns the free start instants on one seller-local date
// (day is midnight in the seller's zone), honouring weekly windows, date
// overrides, notice, horizon, buffer and existing holds or bookings.
func (a *API) sellerDaySlots(ctx context.Context, seller slotSeller, day time.Time, duration int) ([]time.Time, error) {
	location := seller.location
	dateKey := day.Format("2006-01-02")
	today := time.Now().In(location)
	if day.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location)) || day.After(today.AddDate(0, 0, seller.horizon)) {
		return nil, nil
	}
	var windows []availabilityWindow
	weekday := int(day.Weekday())
	rows, err := a.db.Query(ctx, `SELECT local_start::text,local_end::text FROM availability_windows WHERE seller_id=$1 AND weekday=$2 AND timezone=$3 ORDER BY local_start`, seller.id, weekday, seller.zone)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var window availabilityWindow
		window.Weekday = weekday
		if err = rows.Scan(&window.Start, &window.End); err != nil {
			rows.Close()
			return nil, err
		}
		windows = append(windows, window)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var closed bool
	var replacement []byte
	err = a.db.QueryRow(ctx, `SELECT closed,replacement_windows FROM availability_overrides WHERE seller_id=$1 AND local_date=$2`, seller.id, dateKey).Scan(&closed, &replacement)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if closed {
		return nil, nil
	}
	if len(replacement) > 0 && string(replacement) != "null" {
		if err = json.Unmarshal(replacement, &windows); err != nil {
			return nil, err
		}
	}
	if len(windows) == 0 {
		return nil, nil
	}
	rows, err = a.db.Query(ctx, `SELECT lower(occupied_range),upper(occupied_range) FROM slot_reservations WHERE seller_id=$1 AND active AND (reservation_kind='booking' OR expires_at>now()) AND occupied_range && tstzrange($2,$3,'[)')`, seller.id, day.Add(-24*time.Hour), day.AddDate(0, 0, 2))
	if err != nil {
		return nil, err
	}
	type interval struct{ start, end time.Time }
	occupied := []interval{}
	for rows.Next() {
		var x interval
		if err = rows.Scan(&x.start, &x.end); err != nil {
			rows.Close()
			return nil, err
		}
		occupied = append(occupied, x)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// Busy time on the seller's connected Google Calendar blocks slots too.
	busy, err := store.New(a.db).BusyBlocksBetween(ctx, store.BusyBlocksBetweenParams{SellerID: seller.id, FromAt: day.Add(-24 * time.Hour), ToAt: day.AddDate(0, 0, 2)})
	if err != nil {
		return nil, err
	}
	for _, b := range busy {
		occupied = append(occupied, interval{b.StartsAt, b.EndsAt})
	}
	minStart := time.Now().Add(time.Duration(seller.notice) * time.Minute)
	maxStart := time.Now().AddDate(0, 0, seller.horizon)
	out := []time.Time{}
	for _, window := range windows {
		startMinute, ok1 := parseClockMinutes(window.Start)
		endMinute, ok2 := parseClockMinutes(window.End)
		if !ok1 || !ok2 || startMinute >= endMinute {
			continue
		}
		for minute := startMinute; minute+duration+seller.buffer <= endMinute; minute += 15 {
			for _, start := range localInstants(day, minute, location) {
				end := start.Add(time.Duration(duration+seller.buffer) * time.Minute)
				if start.Before(minStart) || start.After(maxStart) || end.In(location).Format("2006-01-02") != dateKey {
					continue
				}
				conflict := false
				for _, busy := range occupied {
					if start.Before(busy.end) && busy.start.Before(end) {
						conflict = true
						break
					}
				}
				if !conflict {
					out = append(out, start)
				}
			}
		}
	}
	return out, nil
}

func localInstants(day time.Time, minute int, location *time.Location) []time.Time {
	year, month, date := day.Date()
	wall := time.Date(year, month, date, minute/60, minute%60, 0, 0, location)
	center := wall.UTC()
	instants := []time.Time{}
	for candidate := center.Add(-4 * time.Hour); !candidate.After(center.Add(4 * time.Hour)); candidate = candidate.Add(15 * time.Minute) {
		local := candidate.In(location)
		if local.Year() == year && local.Month() == month && local.Day() == date && local.Hour() == minute/60 && local.Minute() == minute%60 && local.Second() == 0 {
			instants = append(instants, candidate)
		}
	}
	return instants
}

func hasDuration(durations []int, wanted int) bool {
	for _, d := range durations {
		if d == wanted {
			return true
		}
	}
	return false
}

// validScheduledTime checks a start time against the seller's availability
// and their Google Calendar busy times. ignoreBookingID is the booking being
// moved, whose own calendar event must not block its new time.
func validScheduledTime(ctx context.Context, tx pgx.Tx, sellerID, zone string, local, start time.Time, duration, buffer int, ignoreBookingID *string) bool {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return false
	}
	busy, err := calendarConflict(ctx, tx, sellerID, start, start.Add(time.Duration(duration+buffer)*time.Minute), ignoreBookingID)
	if err != nil || busy {
		return false
	}
	wallMinute := local.Hour()*60 + local.Minute()
	if local.Second() != 0 || local.Nanosecond() != 0 || wallMinute%15 != 0 {
		return false
	}
	validInstant := false
	for _, instant := range localInstants(local, wallMinute, loc) {
		if instant.Equal(start) {
			validInstant = true
			break
		}
	}
	if !validInstant {
		return false
	}
	var closed bool
	var replacement []byte
	err = tx.QueryRow(ctx, `SELECT closed,replacement_windows FROM availability_overrides WHERE seller_id=$1 AND local_date=$2`, sellerID, local.Format("2006-01-02")).Scan(&closed, &replacement)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if closed {
		return false
	}
	var windows []availabilityWindow
	if len(replacement) > 0 && string(replacement) != "null" {
		if json.Unmarshal(replacement, &windows) != nil {
			return false
		}
	} else {
		rows, e := tx.Query(ctx, `SELECT local_start::text,local_end::text FROM availability_windows WHERE seller_id=$1 AND weekday=$2 AND timezone=$3`, sellerID, int(local.Weekday()), zone)
		if e != nil {
			return false
		}
		for rows.Next() {
			var x availabilityWindow
			x.Weekday = int(local.Weekday())
			if rows.Scan(&x.Start, &x.End) != nil {
				rows.Close()
				return false
			}
			windows = append(windows, x)
		}
		if rows.Err() != nil {
			rows.Close()
			return false
		}
		rows.Close()
	}
	end := start.Add(time.Duration(duration+buffer) * time.Minute)
	for _, window := range windows {
		startClock, e1 := time.Parse("15:04:05", window.Start)
		endClock, e2 := time.Parse("15:04:05", window.End)
		if e1 != nil || e2 != nil {
			startClock, e1 = time.Parse("15:04", window.Start)
			endClock, e2 = time.Parse("15:04", window.End)
		}
		if e1 != nil || e2 != nil {
			continue
		}
		startMinute := startClock.Hour()*60 + startClock.Minute()
		endMinute := endClock.Hour()*60 + endClock.Minute()
		if wallMinute < startMinute || wallMinute+duration+buffer > endMinute {
			continue
		}
		for _, windowStart := range localInstants(local, startMinute, loc) {
			for _, windowEnd := range localInstants(local, endMinute, loc) {
				if !windowEnd.After(windowStart) {
					continue
				}
				if !start.Before(windowStart) && !end.After(windowEnd) {
					return true
				}
			}
		}
	}
	return false
}
