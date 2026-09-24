package main

import (
	"bufio"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Local Mailpit is supported without authentication. Use EMAIL_API_KEY through a
// real provider adapter in production; this SMTP helper intentionally refuses auth.
func sendSMTPMessage(addr string, message []byte) error {
	host, port, splitErr := net.SplitHostPort(addr)
	portNum, portErr := strconv.Atoi(port)
	if splitErr != nil || portErr != nil || portNum != 1025 || (host != "127.0.0.1" && host != "localhost" && host != "mailpit") {
		return fmt.Errorf("unauthenticated SMTP is local-development only")
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	c, err := smtp.NewClient(conn, "localhost")
	if err != nil {
		return err
	}
	defer c.Close()
	from := "local@wantmytime.com"
	to := ""
	for _, line := range strings.Split(string(message), "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "from:") {
			v := strings.TrimSpace(line[len("from:"):])
			if i := strings.Index(v, "<"); i >= 0 {
				from = strings.Trim(strings.TrimSuffix(v[i+1:], ">"), " ")
			} else {
				from = v
			}
		}
		if strings.HasPrefix(strings.ToLower(line), "to:") {
			to = strings.TrimSpace(line[len("to:"):])
		}
		if line == "" {
			break // end of headers; never read addresses from the body
		}
	}
	if to == "" {
		return fmt.Errorf("missing recipient")
	}
	if err = c.Mail(from); err != nil {
		return err
	}
	if err = c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	if _, err = bw.Write(message); err != nil {
		return err
	}
	if err = bw.Flush(); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
