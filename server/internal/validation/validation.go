// Package validation centralizes every user-input rule so REST and
// WebSocket entry points enforce identical limits. Handlers call these
// instead of ad-hoc checks; all errors are user-presentable.
package validation

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// Limits (server-authoritative; clients mirror them for feedback only).
const (
	UsernameMin        = 3
	UsernameMax        = 32
	DisplayNameMax     = 64
	StatusMax          = 140
	GroupNameMax       = 100
	DescriptionMax     = 500
	MessageBodyMax     = 2000              // UTF-8 characters, not bytes
	AttachmentMaxBytes = 10 << 20          // 10 MB
	AvatarMaxBytes     = 5 << 20           // 5 MB
	AvatarMaxDimension = 4096              // pixels per side
	PasswordMin        = 8
	PasswordMax        = 128 // bounds Argon2's per-request hashing cost
)

var ErrInvalid = errors.New("validation")

func fail(msg string) error { return fmt.Errorf("%w: %s", ErrInvalid, msg) }

// text is the shared gate for every string field: valid UTF-8 and no
// control characters other than \n and \t.
func text(s, field string) error {
	if !utf8.ValidString(s) {
		return fail(field + " contains invalid UTF-8")
	}
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\t' {
			return fail(field + " contains control characters")
		}
	}
	return nil
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }
