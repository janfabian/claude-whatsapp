package main

import (
	"strings"
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

func TestUniqueMediaSuffix(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want string
	}{
		{"empty", "", ""},
		{"short hex kept whole", "ABC123", "ABC123"},
		{"long id truncated to last 12", "3ABAAAC8F08C54C89020", "F08C54C89020"},
		{"non-alnum stripped", "AC:9F-35/16", "AC9F3516"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := uniqueMediaSuffix(c.id); got != c.want {
				t.Fatalf("uniqueMediaSuffix(%q) = %q, want %q", c.id, got, c.want)
			}
		})
	}
}

// TestMediaFilenameUniquePerMessage is the regression guard for the bug where
// several images sent in the same second shared one filename and overwrote each
// other. Same timestamp + different message IDs must yield different filenames.
func TestMediaFilenameUniquePerMessage(t *testing.T) {
	a := mediaFilename("image", "jpg", "3A1E2B15BC322130C53A")
	b := mediaFilename("image", "jpg", "3AE910DC39D4AB097CBE")
	if a == b {
		t.Fatalf("same-second images collided: both named %q", a)
	}
	for _, name := range []string{a, b} {
		if !strings.HasPrefix(name, "image_") || !strings.HasSuffix(name, ".jpg") {
			t.Fatalf("unexpected filename shape: %q", name)
		}
	}
	// No message ID → legacy timestamp-only name; the suffixed variant adds
	// exactly one extra "_<suffix>" segment before the extension.
	legacy := mediaFilename("video", "mp4", "")
	withID := mediaFilename("video", "mp4", "3AE910DC39D4AB097CBE")
	if strings.Count(withID, "_") != strings.Count(legacy, "_")+1 {
		t.Fatalf("suffixed name %q should have one more underscore than legacy %q", withID, legacy)
	}
}
