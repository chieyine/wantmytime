package main

import (
	"context"
	"strings"
)

// sendEmail sends a plain-text message (sign-in codes, operational alerts).
func (a *API) sendEmail(email, subject, body string) bool {
	if strings.ContainsAny(subject, "\r\n") {
		return false
	}
	return a.deliverEmail(context.Background(), emailMessage{To: email, Subject: subject, Text: body})
}

func sendSMTP(addr string, msg []byte) error { return sendSMTPMessage(addr, msg) }
