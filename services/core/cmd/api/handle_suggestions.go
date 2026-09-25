package main

import (
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Link suggestions, in the spirit of Gmail's: built from the person's name
// (and the link they typed, if it is taken), checked in one query, and only
// free ones returned.

const maxHandleSuggestions = 4

// nameParts lowercases a display name, folds accents ("Adéọlá" -> "adeola")
// and splits it into letters-and-digits words.
func nameParts(name string) []string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(strings.ToLower(name)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// accent marks dropped
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Fields(b.String())
}

// handleCandidates lists links to try, best first. The number suffixes vary
// with the name, so two people called Ada Obi aren't both offered adaobi1.
func handleCandidates(name, wanted string) []string {
	parts := nameParts(name)
	wanted = normalizeHandle(wanted)
	var out []string
	seen := map[string]bool{}
	add := func(h string) {
		if len(h) > 24 {
			h = strings.TrimRight(h[:24], "-")
		}
		if !seen[h] && validHandle(h) {
			seen[h] = true
			out = append(out, h)
		}
	}
	hasher := fnv.New32a()
	hasher.Write([]byte(strings.Join(parts, " ") + "|" + wanted))
	seed := int(hasher.Sum32() % 90)
	numbers := []string{strconv.Itoa(seed%9 + 1), strconv.Itoa(seed + 10), strconv.Itoa((seed+37)%90 + 10), "26"}

	if validHandle(wanted) {
		add(wanted)
	}
	if len(parts) > 0 {
		first, last := parts[0], parts[len(parts)-1]
		joined := strings.Join(parts, "")
		many := len(parts) > 1
		// Most natural first: the whole name, readable with hyphens, the first
		// name alone, then the whole name with a number.
		add(joined)
		add(strings.Join(parts, "-"))
		if many {
			add(first + last)
			add(first + "-" + last)
		}
		add(first)
		add(joined + numbers[0])
		if many {
			add(last + first)
			add(first[:1] + last)
			add(first + "-" + last + numbers[1])
			add(last)
			add(last + "-" + first)
			add(first + last[:1])
		}
		for _, n := range numbers[1:] {
			add(joined + n)
		}
		if many {
			for _, n := range numbers {
				add(first + "-" + last + n)
			}
		}
	}
	// The typed link, made unique the same ways.
	if wanted != "" {
		base := strings.Trim(wanted, "-")
		if len(parts) > 0 && !strings.Contains(base, parts[0]) {
			add(base + "-" + parts[0])
		}
		for _, n := range numbers {
			add(base + n)
		}
	}
	return out
}

// handleSuggestions answers the sign-up and link forms: whether the typed link
// is free, and up to four free links from the person's name.
func (a *API) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	wanted := normalizeHandle(r.URL.Query().Get("want"))
	if len(name) > 80 || len(wanted) > 64 {
		problem(w, 422, "INVALID_QUERY", "That name is too long.")
		return
	}
	candidates := handleCandidates(name, wanted)
	// A seller's own earlier link is free for them to go back to.
	owner := ""
	if u, err := a.currentUser(r); err == nil {
		owner = u.ID
	}
	free := map[string]bool{}
	if len(candidates) > 0 {
		rows, err := a.db.Query(r.Context(), `SELECT h FROM unnest($1::text[]) AS h
			WHERE NOT EXISTS (SELECT 1 FROM seller_profiles WHERE handle=h)
			AND NOT EXISTS (SELECT 1 FROM handle_holds WHERE handle=h AND held_until>now())
			AND NOT EXISTS (SELECT 1 FROM handle_redirects hr JOIN seller_profiles sp ON sp.id=hr.seller_id WHERE hr.old_handle=h AND sp.user_id::text IS DISTINCT FROM NULLIF($2,''))`, candidates, owner)
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "Suggestions are unavailable right now.")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var h string
			if rows.Scan(&h) == nil {
				free[h] = true
			}
		}
	}
	suggestions := []string{}
	numbered := 0
	for _, h := range candidates {
		if !free[h] || h == wanted {
			continue
		}
		// Mostly real words; at most two with numbers, like Gmail.
		if strings.IndexFunc(h, unicode.IsDigit) >= 0 {
			if numbered == 2 {
				continue
			}
			numbered++
		}
		suggestions = append(suggestions, h)
		if len(suggestions) == maxHandleSuggestions {
			break
		}
	}
	out := map[string]any{"suggestions": suggestions}
	if wanted != "" {
		out["wanted"] = map[string]any{"handle": wanted, "valid": validHandle(wanted), "available": free[wanted]}
	}
	jsonOut(w, 200, out)
}
