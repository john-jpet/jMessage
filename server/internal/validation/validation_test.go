package validation

import (
	"strings"
	"testing"
)

func TestUsername(t *testing.T) {
	for _, ok := range []string{"bob", "Alice_99", strings.Repeat("a", 32), "  spaced  "} {
		if _, err := Username(ok); err != nil {
			t.Errorf("Username(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"ab", strings.Repeat("a", 33), "has space", "dot.name", "dash-name", "émoji", ""} {
		if _, err := Username(bad); err == nil {
			t.Errorf("Username(%q) accepted", bad)
		}
	}
	if got, _ := Username("  bob  "); got != "bob" {
		t.Errorf("trim: %q", got)
	}
}

func TestDisplayNameAndStatus(t *testing.T) {
	if _, err := DisplayName(""); err != nil {
		t.Error("empty display name must be allowed (falls back to username)")
	}
	if _, err := DisplayName(strings.Repeat("x", 65)); err == nil {
		t.Error("65-char display name accepted")
	}
	if _, err := DisplayName("ok\x00name"); err == nil {
		t.Error("control character accepted")
	}
	if _, err := Status(strings.Repeat("s", 140)); err != nil {
		t.Error("140-char status rejected")
	}
	if _, err := Status(strings.Repeat("s", 141)); err == nil {
		t.Error("141-char status accepted")
	}
	if _, err := Status(string([]byte{0xff, 0xfe})); err == nil {
		t.Error("invalid UTF-8 status accepted")
	}
}

func TestMessageBody(t *testing.T) {
	if err := MessageBody("", false); err == nil {
		t.Error("empty body without attachments accepted")
	}
	if err := MessageBody("", true); err != nil {
		t.Error("empty body WITH attachments rejected:", err)
	}
	// 2000 multi-byte characters are fine even though they exceed 2000 bytes.
	if err := MessageBody(strings.Repeat("é", 2000), false); err != nil {
		t.Error("2000 multibyte chars rejected:", err)
	}
	if err := MessageBody(strings.Repeat("a", 2001), false); err == nil {
		t.Error("2001 chars accepted")
	}
	if err := MessageBody(string([]byte{0x80, 0x81}), false); err == nil {
		t.Error("malformed UTF-8 accepted")
	}
	if err := MessageBody("tabs\tand\nnewlines ok", false); err != nil {
		t.Error("tabs/newlines rejected:", err)
	}
}

func TestEmoji(t *testing.T) {
	for _, e := range ReactionEmojis {
		if err := Emoji(e); err != nil {
			t.Errorf("Emoji(%q) = %v", e, err)
		}
	}
	for _, bad := range []string{"🎉", "thumbsup", "", "👍👍"} {
		if err := Emoji(bad); err == nil {
			t.Errorf("Emoji(%q) accepted", bad)
		}
	}
}

func TestGroupName(t *testing.T) {
	if _, err := GroupName(strings.Repeat("g", 100)); err != nil {
		t.Error("100-char group name rejected")
	}
	if _, err := GroupName(strings.Repeat("g", 101)); err == nil {
		t.Error("101-char group name accepted")
	}
	if _, err := GroupName("   "); err == nil {
		t.Error("whitespace-only group name accepted")
	}
}

func TestAvatarRules(t *testing.T) {
	if err := AvatarMime("image/png"); err != nil {
		t.Error(err)
	}
	if err := AvatarMime("image/svg+xml"); err == nil {
		t.Error("svg avatar accepted")
	}
	if err := AvatarDimensions(4096, 4096); err != nil {
		t.Error(err)
	}
	if err := AvatarDimensions(4097, 100); err == nil {
		t.Error("oversized avatar dimensions accepted")
	}
	if err := AvatarDimensions(0, 10); err == nil {
		t.Error("zero-width avatar accepted")
	}
}
