package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// redisClient is a small RESP2 client for the few commands WantMyTime needs
// (shared rate limits). It keeps a bounded pool of connections, supports
// redis:// and rediss:// (TLS) URLs with a password, and never blocks a
// request for longer than its timeout.
type redisClient struct {
	addr     string
	username string
	password string
	db       int
	useTLS   bool
	host     string
	timeout  time.Duration
	pool     chan *redisConn
}

type redisConn struct {
	c net.Conn
	r *bufio.Reader
}

var errRedisNil = errors.New("redis: nil")

type redisError string

func (e redisError) Error() string { return "redis: " + string(e) }

func newRedisClient(raw string) (*redisClient, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "redis" && u.Scheme != "rediss") || u.Host == "" {
		return nil, fmt.Errorf("REDIS_URL must look like redis://[:password@]host:port[/db] or rediss://")
	}
	c := &redisClient{useTLS: u.Scheme == "rediss", timeout: 250 * time.Millisecond, pool: make(chan *redisConn, 16)}
	c.host = u.Hostname()
	c.addr = u.Host
	if u.Port() == "" {
		c.addr = net.JoinHostPort(c.host, "6379")
	}
	if u.User != nil {
		c.username = u.User.Username()
		c.password, _ = u.User.Password()
		if c.password == "" && c.username != "" {
			// redis://secret@host is the common "password only" form.
			c.password, c.username = c.username, ""
		}
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		if c.db, err = strconv.Atoi(p); err != nil || c.db < 0 {
			return nil, fmt.Errorf("REDIS_URL database must be a number")
		}
	}
	return c, nil
}

func (c *redisClient) dial(ctx context.Context) (*redisConn, error) {
	d := net.Dialer{Timeout: c.timeout}
	var conn net.Conn
	var err error
	if c.useTLS {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: c.host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", c.addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", c.addr)
	}
	if err != nil {
		return nil, err
	}
	rc := &redisConn{c: conn, r: bufio.NewReader(conn)}
	if c.password != "" {
		args := []string{"AUTH", c.password}
		if c.username != "" {
			args = []string{"AUTH", c.username, c.password}
		}
		if _, err = rc.do(c.timeout, args...); err != nil {
			conn.Close()
			return nil, err
		}
	}
	if c.db != 0 {
		if _, err = rc.do(c.timeout, "SELECT", strconv.Itoa(c.db)); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return rc, nil
}

// do runs one command. A connection that fails is dropped, not reused.
func (c *redisClient) do(ctx context.Context, args ...string) (any, error) {
	var conn *redisConn
	select {
	case conn = <-c.pool:
	default:
		var err error
		if conn, err = c.dial(ctx); err != nil {
			return nil, err
		}
	}
	out, err := conn.do(c.timeout, args...)
	var rerr redisError
	if err != nil && !errors.As(err, &rerr) && !errors.Is(err, errRedisNil) {
		conn.c.Close()
		return nil, err
	}
	select {
	case c.pool <- conn:
	default:
		conn.c.Close()
	}
	return out, err
}

func (rc *redisConn) do(timeout time.Duration, args ...string) (any, error) {
	_ = rc.c.SetDeadline(time.Now().Add(timeout))
	var b strings.Builder
	b.WriteString("*" + strconv.Itoa(len(args)) + "\r\n")
	for _, a := range args {
		b.WriteString("$" + strconv.Itoa(len(a)) + "\r\n" + a + "\r\n")
	}
	if _, err := rc.c.Write([]byte(b.String())); err != nil {
		return nil, err
	}
	return readRESP(rc.r)
}

func readRESP(r *bufio.Reader) (any, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 3 || !strings.HasSuffix(line, "\r\n") {
		return nil, errors.New("redis: malformed reply")
	}
	body := line[1 : len(line)-2]
	switch line[0] {
	case '+':
		return body, nil
	case '-':
		return nil, redisError(body)
	case ':':
		return strconv.ParseInt(body, 10, 64)
	case '$':
		n, err := strconv.Atoi(body)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, errRedisNil
		}
		if n > 1<<20 {
			return nil, errors.New("redis: reply too large")
		}
		buf := make([]byte, n+2)
		if _, err = io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(body)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, errRedisNil
		}
		if n > 1024 {
			return nil, errors.New("redis: reply too large")
		}
		items := make([]any, 0, n)
		for i := 0; i < n; i++ {
			v, err := readRESP(r)
			if err != nil && !errors.Is(err, errRedisNil) {
				return nil, err
			}
			items = append(items, v)
		}
		return items, nil
	}
	return nil, errors.New("redis: unknown reply type")
}

func (c *redisClient) ping(ctx context.Context) error {
	v, err := c.do(ctx, "PING")
	if err != nil {
		return err
	}
	if v != "PONG" {
		return errors.New("redis: unexpected PING reply")
	}
	return nil
}

// Fixed-window counter: the first hit in a window sets its expiry.
const rateScript = `local n=redis.call('INCR',KEYS[1]) if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end return n`

func (c *redisClient) incrWindow(ctx context.Context, key string, window time.Duration) (int64, error) {
	v, err := c.do(ctx, "EVAL", rateScript, "1", key, strconv.FormatInt(window.Milliseconds(), 10))
	if err != nil {
		return 0, err
	}
	n, ok := v.(int64)
	if !ok {
		return 0, errors.New("redis: unexpected counter reply")
	}
	return n, nil
}
