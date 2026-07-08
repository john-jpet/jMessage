// Package ws implements the real-time layer: the WebSocket endpoint,
// per-connection clients, and the hub that tracks online users and
// fans frames out to them.
//
// Wire protocol: JSON frames discriminated by "type".
//
//	client → server:  send{tempID, convID, body} · typing{convID} · ping
//	server → client:  ack{tempID, convID, seq, ts} · message{convID,
//	                  seq, senderID, body, ts} · typing{convID, userID} ·
//	                  presence{userID, online} · error{code, message,
//	                  tempID?} · pong
package ws

import "encoding/json"

// Frame is the single wire shape for both directions; unused fields are
// omitted from the JSON.
type Frame struct {
	Type     string `json:"type"`
	TempID   string `json:"tempID,omitempty"`
	ConvID   string `json:"convID,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
	SenderID string `json:"senderID,omitempty"`
	Body     string `json:"body,omitempty"`
	UserID   string `json:"userID,omitempty"`
	Online   *bool  `json:"online,omitempty"`
	TS       int64  `json:"ts,omitempty"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
}

const (
	TypeSend     = "send"
	TypeTyping   = "typing"
	TypePing     = "ping"
	TypeAck      = "ack"
	TypeMessage  = "message"
	TypePresence = "presence"
	TypeError    = "error"
	TypePong     = "pong"
)

// maxBodyBytes bounds one message body (also enforced client-side).
const maxBodyBytes = 8 << 10

func encode(f Frame) []byte {
	b, _ := json.Marshal(f)
	return b
}

func errorFrame(code, msg, tempID string) []byte {
	return encode(Frame{Type: TypeError, Code: code, Message: msg, TempID: tempID})
}
