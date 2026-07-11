package validation

import "strings"

// GroupName validates and returns the trimmed group conversation name.
func GroupName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if err := text(s, "group name"); err != nil {
		return "", err
	}
	if s == "" {
		return "", fail("group conversations need a name")
	}
	if runeLen(s) > GroupNameMax {
		return "", fail("group name must be at most 100 characters")
	}
	return s, nil
}

// Description validates a conversation description (reserved for a
// future feature; enforced now for forward compatibility).
func Description(s string) (string, error) {
	s = strings.TrimSpace(s)
	if err := text(s, "description"); err != nil {
		return "", err
	}
	if runeLen(s) > DescriptionMax {
		return "", fail("description must be at most 500 characters")
	}
	return s, nil
}
