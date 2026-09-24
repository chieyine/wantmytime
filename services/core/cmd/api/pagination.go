package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const operationsPageSize = 100

type listCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func readListCursor(r *http.Request) (*time.Time, *string, error) {
	token := r.URL.Query().Get("cursor")
	if token == "" {
		return nil, nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, nil, errors.New("invalid cursor")
	}
	var cursor listCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.At.IsZero() {
		return nil, nil, errors.New("invalid cursor")
	}
	compactID := strings.ReplaceAll(cursor.ID, "-", "")
	if len(cursor.ID) != 36 || len(compactID) != 32 {
		return nil, nil, errors.New("invalid cursor")
	}
	if _, err := hex.DecodeString(compactID); err != nil {
		return nil, nil, errors.New("invalid cursor")
	}
	id := cursor.ID
	at := cursor.At
	return &at, &id, nil
}

func encodeListCursor(at time.Time, id string) string {
	data, _ := json.Marshal(listCursor{At: at.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(data)
}

func nextListCursor(items []map[string]any, timeField, idField string) string {
	if len(items) < operationsPageSize {
		return ""
	}
	last := items[len(items)-1]
	at, okAt := last[timeField].(time.Time)
	id, okID := last[idField].(string)
	if !okAt || !okID || at.IsZero() || id == "" {
		return ""
	}
	return encodeListCursor(at, id)
}

func cursorForRequest(w http.ResponseWriter, r *http.Request) (*time.Time, *string, bool) {
	at, id, err := readListCursor(r)
	if err != nil {
		problem(w, 400, "INVALID_CURSOR", "Refresh the list and try again.")
		return nil, nil, false
	}
	return at, id, true
}
