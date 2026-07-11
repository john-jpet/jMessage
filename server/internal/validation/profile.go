package validation

import (
	"regexp"
	"strings"
)

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// Username validates and returns the trimmed username. Usernames are
// immutable after registration.
func Username(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := runeLen(s); n < UsernameMin || n > UsernameMax {
		return "", fail("username must be 3-32 characters")
	}
	if !usernameRE.MatchString(s) {
		return "", fail("username may only contain letters, numbers, and underscores")
	}
	return s, nil
}

// DisplayName validates and returns the trimmed display name. Empty is
// allowed: the application falls back to the username.
func DisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if err := text(s, "display name"); err != nil {
		return "", err
	}
	if runeLen(s) > DisplayNameMax {
		return "", fail("display name must be at most 64 characters")
	}
	return s, nil
}

// Status validates and returns the trimmed status text (empty clears it).
func Status(s string) (string, error) {
	s = strings.TrimSpace(s)
	if err := text(s, "status"); err != nil {
		return "", err
	}
	if runeLen(s) > StatusMax {
		return "", fail("status must be at most 140 characters")
	}
	return s, nil
}

// AvatarMimes are the accepted avatar content types (std-lib decodable,
// so the server can verify dimensions from the header alone).
var AvatarMimes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
}

// AvatarMime checks a sniffed content type against the allow-list.
func AvatarMime(mime string) error {
	if !AvatarMimes[mime] {
		return fail("avatar must be a PNG, JPEG, or GIF image")
	}
	return nil
}

// AvatarDimensions checks decoded image bounds.
func AvatarDimensions(w, h int) error {
	if w < 1 || h < 1 || w > AvatarMaxDimension || h > AvatarMaxDimension {
		return fail("avatar dimensions must be between 1x1 and 4096x4096")
	}
	return nil
}
