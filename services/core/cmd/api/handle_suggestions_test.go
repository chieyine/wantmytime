package main

import (
	"strings"
	"testing"
)

func TestHandleCandidatesFromName(t *testing.T) {
	got := handleCandidates("Adéọlá Obi", "")
	if len(got) < 10 {
		t.Fatalf("too few candidates: %v", got)
	}
	// Best first: the whole name, then the readable hyphenated form.
	if got[0] != "adeolaobi" || got[1] != "adeola-obi" {
		t.Fatalf("order %v", got[:4])
	}
	for _, want := range []string{"aobi", "adeolao", "obiadeola", "adeola", "obi"} {
		if !contains(got, want) {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	for _, h := range got {
		if !validHandle(h) {
			t.Errorf("invalid candidate %q", h)
		}
	}
}

func TestHandleCandidatesSkipReservedAndLong(t *testing.T) {
	got := handleCandidates("Help", "")
	if contains(got, "help") {
		t.Fatalf("reserved word offered: %v", got)
	}
	long := handleCandidates("Oluwaseunfunmilayo Adebayo-Ogunleye", "")
	for _, h := range long {
		if len(h) > 24 || strings.HasSuffix(h, "-") {
			t.Fatalf("bad long candidate %q", h)
		}
	}
}

func TestHandleCandidatesVaryByPerson(t *testing.T) {
	a, b := handleCandidates("Ada Obi", ""), handleCandidates("Ada Obi", "adaobi-coach")
	if strings.Join(a, ",") == strings.Join(b, ",") {
		t.Fatal("a typed link should change the suggestions")
	}
	if !contains(b, "adaobi-coach") || !strings.HasPrefix(b[0], "adaobi-coach") {
		t.Fatalf("typed link should lead: %v", b[:3])
	}
}
