package validation

// MessageBody validates a message body. Empty bodies are allowed only
// when the message carries attachments.
func MessageBody(s string, hasAttachments bool) error {
	if err := text(s, "message"); err != nil {
		return err
	}
	if s == "" && !hasAttachments {
		return fail("message is empty")
	}
	if runeLen(s) > MessageBodyMax {
		return fail("message must be at most 2000 characters")
	}
	return nil
}

// ReactionEmojis is the supported set (custom emojis are out of scope).
var ReactionEmojis = []string{"👍", "❤️", "😂", "😮", "😢", "👎"}

var reactionSet = func() map[string]bool {
	m := make(map[string]bool, len(ReactionEmojis))
	for _, e := range ReactionEmojis {
		m[e] = true
	}
	return m
}()

// Emoji checks membership in the supported reaction set.
func Emoji(s string) error {
	if !reactionSet[s] {
		return fail("unsupported reaction emoji")
	}
	return nil
}
