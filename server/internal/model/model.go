// Package model defines the persistent records and API shapes shared
// across the server. Persistent structs are stored as JSON values in
// PetDB; summaries are what the API returns (never the password hash).
package model

// User is the stored user record (value of user:<id>).
type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"displayName"` // optional: empty shows username
	Status       string `json:"status,omitempty"`
	AvatarID     string `json:"avatarID,omitempty"` // attachment in state "avatar"
	PasswordHash string `json:"passwordHash"`
	CreatedAt    int64  `json:"createdAt"` // unix ms
}

// UserSummary is the API-safe projection of a User.
type UserSummary struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status,omitempty"`
	AvatarID    string `json:"avatarID,omitempty"`
	Online      bool   `json:"online,omitempty"`
}

// Name is the presentation name: display name when set, else username.
func (u *User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func (u *User) Summary() UserSummary {
	return UserSummary{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.Name(),
		Status:      u.Status,
		AvatarID:    u.AvatarID,
	}
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
// rebuilt from message docs during allocator recovery. Attachments hold
// render metadata only — blob bytes never live in PetDB. Reactions are
// NEVER persisted here: the field is populated at response time from
// the reaction: keyspace (message docs are written before any reaction
// can exist).
type Message struct {
	ConvID      string          `json:"convID"`
	Seq         uint64          `json:"seq"`
	SenderID    string          `json:"senderID"`
	Body        string          `json:"body"`
	TS          int64           `json:"ts"` // unix ms
	ClientMsgID string          `json:"clientMsgID,omitempty"`
	Attachments []AttachmentRef `json:"attachments,omitempty"`
	Reactions   []ReactionAgg   `json:"reactions,omitempty"`
}

// ReactionAgg is one emoji's aggregate on a message, viewer-relative
// (Reacted reflects the requesting user).
type ReactionAgg struct {
	Emoji   string `json:"emoji"`
	Count   int    `json:"count"`
	Reacted bool   `json:"reacted,omitempty"`
}

// Attachment lifecycle states.
const (
	AttachmentPending   = "pending"   // uploaded, not yet referenced by a message
	AttachmentCommitted = "committed" // referenced by a message in ConvID
	AttachmentAvatar    = "avatar"    // referenced by its owner's profile
	AttachmentDeleted   = "deleted"
)

// Attachment is the stored metadata record (value of attachment:<id>).
// The bytes live in the blob store at blobs/<id>.bin. ConvID is stamped
// when a message commits the attachment and is the authorization root
// for downloads.
type Attachment struct {
	ID        string `json:"id"`
	OwnerID   string `json:"ownerID"`
	ConvID    string `json:"convID,omitempty"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mimeType"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	State     string `json:"state"`
	CreatedAt int64  `json:"createdAt"`           // unix ms
	ExpiresAt int64  `json:"expiresAt,omitempty"` // future: expiry enforcement
}

// AttachmentRef is the render metadata embedded in messages and frames.
type AttachmentRef struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

func (a *Attachment) Ref() AttachmentRef {
	return AttachmentRef{ID: a.ID, Filename: a.Filename, MimeType: a.MimeType, Size: a.Size}
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
