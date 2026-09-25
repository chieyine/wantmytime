package main

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRESPReplies(t *testing.T) {
	for raw, want := range map[string]any{"+PONG\r\n": "PONG", ":42\r\n": int64(42), "$3\r\nabc\r\n": "abc"} {
		got, err := readRESP(bufio.NewReader(strings.NewReader(raw)))
		if err != nil || got != want {
			t.Fatalf("%q: got %v, %v", raw, got, err)
		}
	}
	if _, err := readRESP(bufio.NewReader(strings.NewReader("-ERR wrong\r\n"))); err == nil || !strings.Contains(err.Error(), "ERR wrong") {
		t.Fatalf("error reply not surfaced: %v", err)
	}
	if _, err := readRESP(bufio.NewReader(strings.NewReader("$-1\r\n"))); err != errRedisNil {
		t.Fatalf("nil reply: %v", err)
	}
	if _, err := readRESP(bufio.NewReader(strings.NewReader("$99999999\r\n"))); err == nil {
		t.Fatal("oversized reply accepted")
	}
}

func TestRedisURLParsing(t *testing.T) {
	c, err := newRedisClient("rediss://:s3cret@cache.example.com:6380/2")
	if err != nil || !c.useTLS || c.password != "s3cret" || c.db != 2 || c.addr != "cache.example.com:6380" {
		t.Fatalf("parsed %+v, %v", c, err)
	}
	c, err = newRedisClient("redis://token@cache.example.com")
	if err != nil || c.password != "token" || c.username != "" || c.addr != "cache.example.com:6379" {
		t.Fatalf("password-only form parsed %+v, %v", c, err)
	}
	for _, bad := range []string{"http://x", "redis://", "redis://h:1/notanumber"} {
		if _, err := newRedisClient(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestRateLimitFallsBackWhenRedisIsDown(t *testing.T) {
	rc, _ := newRedisClient("redis://127.0.0.1:1")
	failures := 0
	l := newRateLimiter(2, time.Minute)
	l.shared, l.onFail = rc, func(error) { failures++ }
	now := time.Now()
	if !l.allow("ip", now) || !l.allow("ip", now) || l.allow("ip", now) {
		t.Fatal("local fallback did not enforce the limit")
	}
	if failures != 3 {
		t.Fatalf("Redis failures reported %d times, want 3", failures)
	}
}

func TestSharedRateLimitAcrossInstances(t *testing.T) {
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL is not set")
	}
	rc, err := newRedisClient(url)
	if err != nil {
		t.Fatal(err)
	}
	if err = rc.ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	name := "test" + time.Now().Format("150405.000000")
	first, second := newRateLimiter(3, time.Minute), newRateLimiter(3, time.Minute)
	for _, l := range []*rateLimiter{first, second} {
		l.name, l.shared = name, rc
		l.onFail = func(err error) { t.Fatalf("Redis failed: %v", err) }
	}
	now := time.Now()
	if !first.allow("203.0.113.9", now) || !second.allow("203.0.113.9", now) || !first.allow("203.0.113.9", now) {
		t.Fatal("hits under the limit were refused")
	}
	if second.allow("203.0.113.9", now) {
		t.Fatal("the fourth hit across two instances was allowed")
	}
	if !second.allow("198.51.100.1", now) {
		t.Fatal("another address shared the count")
	}
	if strings.Contains(first.sharedKey("203.0.113.9", now), "203.0.113.9") {
		t.Fatal("Redis key holds the raw address")
	}
}
