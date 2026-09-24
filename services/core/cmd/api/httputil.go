package main

import (
	"encoding/json"
	"errors"
	_ "image/jpeg"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
)

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func problem(w http.ResponseWriter, status int, code, message string) {
	var b errorBody
	b.Error.Code = code
	b.Error.Message = message
	jsonOut(w, status, b)
}

func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 32<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func pgErrCode(e error) string {
	var pe *pgconn.PgError
	if errors.As(e, &pe) {
		return pe.Code
	}
	return ""
}

// nullableString maps "" to SQL NULL.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
