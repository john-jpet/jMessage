// Package ws implements the real-time layer: the WebSocket endpoint,
// per-connection clients, and the hub that tracks online users and
// fans frames out to them.
//
// Wire protocol: JSON frames discriminated by "type".
//
//	client → server:  send{tempID, convID, body} · typing{convID} ·
//	                  resume{lastSeen} · read{convID, seq} ·
//	                  delivered{convID, seq} · ping
//	server → client:  ack{tempID, convID, seq, ts} · message{convID,
//	                  seq, senderID, body, ts} · typing{convID, userID} ·
//	                  presence{userID, online} · receipt{convID, userID,
//	                  deliveredSeq, readSeq} · error{code, message,
//	                  tempID?} · pong
//
// send.tempID is the client-generated idempotency key (clientMsgID):
// retrying a send frame acks the originally assigned seq, never a dup.
package ws

import (
	"encoding/json"

	"jmessage/internal/model"
)

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

	// Tier 1 reliability fields.
	LastSeen     map[string]uint64 `json:"lastSeen,omitempty"` // resume: convID -> last seq held
	DeliveredSeq uint64            `json:"deliveredSeq,omitempty"`
	ReadSeq      uint64            `json:"readSeq,omitempty"`

	// Tier 2 attachment fields: IDs client→server on send; full render
	// refs server→client on ack/message.
	AttachmentIDs []string              `json:"attachmentIDs,omitempty"`
	Attachments   []model.AttachmentRef `json:"attachments,omitempty"`

	// Tier 4 reaction fields. Count is a pointer so zero (last removal)
	// still serializes on reaction_update frames.
	Emoji string `json:"emoji,omitempty"`
	Count *int   `json:"count,omitempty"`
	Added bool   `json:"added,omitempty"`
}

const (
	TypeSend      = "send"
	TypeTyping    = "typing"
	TypePing      = "ping"
	TypeResume    = "resume"
	TypeRead      = "read"
	TypeDelivered = "delivered"
	TypeAck       = "ack"
	TypeMessage   = "message"
	TypePresence  = "presence"
	TypeReceipt   = "receipt"
	TypeError     = "error"
	TypePong      = "pong"

	TypeReactionAdd    = "reaction_add"
	TypeReactionRemove = "reaction_remove"
	TypeReactionUpdate = "reaction_update"
)

// maxAttachments bounds attachments per message. Body limits live in
// internal/validation (shared with REST).
const maxAttachments = 10

func encode(f Frame) []byte {
	b, _ := json.Marshal(f)
	return b
}

func errorFrame(code, msg, tempID string) []byte {
	return encode(Frame{Type: TypeError, Code: code, Message: msg, TempID: tempID})
}
