package store

import (
	"fmt"
	"strconv"
	"strings"
)

// Key layout. All IDs are fixed-width zero-padded decimals so keys
// split unambiguously on ':' and sort lexicographically in numeric
// order. userID/convID = 10 digits; message seq = 14 digits (spec).
//
//	user:<uid>                     user record
//	username:<lowercased>          username -> userID (uniqueness index)
//	conversation:<cid>             conversation metadata + last activity
//	member:<cid>:<uid>             membership (authorization truth)
//	usermember:<uid>:<cid>         reverse index: my conversations
//	dm:<minUID>:<maxUID>           DM dedup -> convID
//	message:<cid>:<seq>            message timeline (dense seqs)
//	convseq:<cid>                  last-seq hint for the allocator
//	counter:user, counter:conv     global ID hints
//	clientmsg:<uid>:<clientMsgID>  send idempotency -> {convID, seq, ts}
//	read:<uid>:<cid>               delivered/read watermarks
//	device:<uid>:<deviceID>        device registry
//	attachment:<uuid>              attachment metadata (bytes in blobs/)

func pad10(n uint64) string { return fmt.Sprintf("%010d", n) }
func pad14(n uint64) string { return fmt.Sprintf("%014d", n) }

func userKey(uid string) []byte      { return []byte("user:" + uid) }
func usernameKey(name string) []byte { return []byte("username:" + strings.ToLower(name)) }
func convKey(cid string) []byte      { return []byte("conversation:" + cid) }

func memberKey(cid, uid string) []byte     { return []byte("member:" + cid + ":" + uid) }
func memberPrefix(cid string) []byte       { return []byte("member:" + cid + ":") }
func userMemberKey(uid, cid string) []byte { return []byte("usermember:" + uid + ":" + cid) }
func userMemberPrefix(uid string) []byte   { return []byte("usermember:" + uid + ":") }

// dmKey orders the pair so both directions map to the same key.
func dmKey(a, b string) []byte {
	if a > b {
		a, b = b, a
	}
	return []byte("dm:" + a + ":" + b)
}

func msgKey(cid string, seq uint64) []byte { return []byte("message:" + cid + ":" + pad14(seq)) }
func msgPrefix(cid string) []byte          { return []byte("message:" + cid + ":") }
func convSeqKey(cid string) []byte         { return []byte("convseq:" + cid) }

func clientMsgKey(uid, clientMsgID string) []byte {
	return []byte("clientmsg:" + uid + ":" + clientMsgID)
}

const clientMsgPrefix = "clientmsg:"

func readStateKey(uid, cid string) []byte { return []byte("read:" + uid + ":" + cid) }

func deviceKey(uid, deviceID string) []byte { return []byte("device:" + uid + ":" + deviceID) }
func devicePrefix(uid string) []byte        { return []byte("device:" + uid + ":") }

func attachmentKey(id string) []byte { return []byte("attachment:" + id) }

const attachmentPrefix = "attachment:"

// Reactions: reaction:<cid>:<seq14>:<emoji>:<uid> -> {}. Fixed-width
// cid/seq keep all of one conversation's reactions in one seq-ordered
// range, so a history page aggregates with a single scan.
func reactionKey(cid string, seq uint64, emoji, uid string) []byte {
	return []byte("reaction:" + cid + ":" + pad14(seq) + ":" + emoji + ":" + uid)
}

func reactionEmojiPrefix(cid string, seq uint64, emoji string) []byte {
	return []byte("reaction:" + cid + ":" + pad14(seq) + ":" + emoji + ":")
}

func reactionSeqPrefix(cid string, seq uint64) []byte {
	return []byte("reaction:" + cid + ":" + pad14(seq) + ":")
}

// prefixEnd returns the exclusive upper bound for a prefix scan: the
// prefix with its last byte incremented. All key bytes here are ASCII,
// so overflow is impossible.
func prefixEnd(prefix []byte) []byte {
	end := append([]byte(nil), prefix...)
	end[len(end)-1]++
	return end
}

// lastSegment returns the text after the final ':' in a key — e.g. the
// convID from a usermember key or the userID from a member key.
func lastSegment(key []byte) string {
	s := string(key)
	return s[strings.LastIndexByte(s, ':')+1:]
}

// seqFromMsgKey parses the 14-digit sequence at the tail of a message key.
func seqFromMsgKey(key []byte) (uint64, error) {
	return strconv.ParseUint(lastSegment(key), 10, 64)
}
