package main

import (
	"testing"
	"time"
)

func TestSessionFilterMatches(t *testing.T) {
	cases := []struct {
		name   string
		filter *sessionFilter
		jid    string
		want   bool
	}{
		{"nil filter matches all", nil, "1@s.whatsapp.net", true},
		{"empty chats matches all", &sessionFilter{}, "1@g.us", true},
		{"chats allowlist hit", &sessionFilter{Chats: []string{"g1@g.us"}}, "g1@g.us", true},
		{"chats allowlist miss", &sessionFilter{Chats: []string{"g1@g.us"}}, "g2@g.us", false},
		{"excludeChats overrides allowlist", &sessionFilter{Chats: []string{"g1@g.us"}, ExcludeChats: []string{"g1@g.us"}}, "g1@g.us", false},
		{"excludeChats with empty allowlist", &sessionFilter{ExcludeChats: []string{"g2@g.us"}}, "g2@g.us", false},
		{"excludeChats does not block others", &sessionFilter{ExcludeChats: []string{"g2@g.us"}}, "g3@g.us", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.filter.matches(c.jid); got != c.want {
				t.Fatalf("matches(%q) = %v, want %v", c.jid, got, c.want)
			}
		})
	}
}

func TestClaimRegistryFirstClaim(t *testing.T) {
	r := newClaimRegistry()
	if err := r.tryClaim("g1@g.us", "clientA"); err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
}

func TestClaimRegistryConflict(t *testing.T) {
	r := newClaimRegistry()
	_ = r.tryClaim("g1@g.us", "clientA")
	if err := r.tryClaim("g1@g.us", "clientB"); err == nil {
		t.Fatalf("expected conflict, got nil")
	}
}

func TestClaimRegistrySameClientReclaim(t *testing.T) {
	r := newClaimRegistry()
	_ = r.tryClaim("g1@g.us", "clientA")
	if err := r.tryClaim("g1@g.us", "clientA"); err != nil {
		t.Fatalf("same-client reclaim failed: %v", err)
	}
}

func TestClaimRegistryReleaseChatAllowsRecaim(t *testing.T) {
	r := newClaimRegistry()
	_ = r.tryClaim("g1@g.us", "clientA")
	r.releaseChat("g1@g.us", "clientA")
	if err := r.tryClaim("g1@g.us", "clientB"); err != nil {
		t.Fatalf("after releaseChat clientB should claim: %v", err)
	}
}

func TestClaimRegistryGraceReclaimSameClient(t *testing.T) {
	r := newClaimRegistry()
	_ = r.tryClaim("g1@g.us", "clientA")
	r.release("clientA") // disconnect; claim in grace
	if err := r.tryClaim("g1@g.us", "clientA"); err != nil {
		t.Fatalf("clientA should reclaim its own grace claim: %v", err)
	}
}

func TestClaimRegistryGraceBlocksOtherClient(t *testing.T) {
	r := newClaimRegistry()
	_ = r.tryClaim("g1@g.us", "clientA")
	r.release("clientA")
	if err := r.tryClaim("g1@g.us", "clientB"); err == nil {
		t.Fatalf("clientB should NOT claim during grace window")
	}
}

func TestClaimRegistryGraceExpiry(t *testing.T) {
	r := newClaimRegistry()
	_ = r.tryClaim("g1@g.us", "clientA")
	// Manually expire the claim by backdating releasedAt past the grace window.
	r.mu.Lock()
	r.claims["g1@g.us"].releasedAt = time.Now().Add(-2 * claimGrace)
	r.mu.Unlock()
	if err := r.tryClaim("g1@g.us", "clientB"); err != nil {
		t.Fatalf("expired claim should be reclaimable by clientB: %v", err)
	}
}

func TestIsValidJID(t *testing.T) {
	good := []string{
		"1234567890@s.whatsapp.net",
		"120363100000000000@g.us",
		"status@broadcast",
		"abc.def-ghi@newsletter",
		"42@lid",
	}
	bad := []string{
		"",
		"nojid",
		"foo@bar.com",
		"1234@s.whatsapp.net/extra",
		"1234 5678@s.whatsapp.net",
	}
	for _, s := range good {
		if !isValidJID(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	for _, s := range bad {
		if isValidJID(s) {
			t.Errorf("expected %q invalid", s)
		}
	}
}
