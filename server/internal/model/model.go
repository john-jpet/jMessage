// Package model defines the persistent records and API shapes shared
// across the server. Persistent structs are stored as JSON values in
// PetDB; summaries are what the API returns (never the password hash).
package model

// User is the stored user record (value of user:<id>).
type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"displayName"`
	PasswordHash string `json:"passwordHash"`
	CreatedAt    int64  `json:"createdAt"` // unix ms
}

// UserSummary is the API-safe projection of a User.
type UserSummary struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Online      bool   `json:"online,omitempty"`
}

func (u *User) Summary() UserSummary {
	return UserSummary{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName}
}

// Conversation types.
const (
	ConvDM    = "dm"
	ConvGroup = "group"
)

// Conversation is the stored conversation record (value of
// conversation:<id>). It is rewritten on every message to refresh the
// last-activity fields, so it deliberately does not hold the member
// list (members live in member:<cid>:<uid> keys).
type Conversation struct {
	ID             string `json:"id"`
	Type           string `json:"type"` // dm | group
	Name           string `json:"name,omitempty"`
	CreatorID      string `json:"creatorID"`
	CreatedAt      int64  `json:"createdAt"`
	LastActivityTS int64  `json:"lastActivityTS"`
	LastPreview    string `json:"lastPreview,omitempty"`
	LastSenderID   string `json:"lastSenderID,omitempty"`
}

// Member is the stored membership record (value of member:<cid>:<uid>).
type Member struct {
	JoinedAt int64 `json:"joinedAt"`
}

// Message is the stored message record (value of message:<cid>:<seq>).
// ClientMsgID is embedded so the clientmsg: idempotency index can be
// rebuilt from message docs during allocator recovery.
type Message struct {
	ConvID      string `json:"convID"`
	Seq         uint64 `json:"seq"`
	SenderID    string `json:"senderID"`
	Body        string `json:"body"`
	TS          int64  `json:"ts"` // unix ms
	ClientMsgID string `json:"clientMsgID,omitempty"`
}

// ReadState is the per-(user, conversation) watermark record (value of
// read:<uid>:<cid>). Both sequences are monotonic: a message with
// seq <= DeliveredSeq has reached at least one of the user's devices;
// seq <= ReadSeq has been seen.
type ReadState struct {
	DeliveredSeq uint64 `json:"deliveredSeq"`
	ReadSeq      uint64 `json:"readSeq"`
}

// Device is a registered client device (value of device:<uid>:<deviceID>).
type Device struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LastSeen int64  `json:"lastSeen"` // unix ms
}
