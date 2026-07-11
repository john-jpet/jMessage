export interface UserSummary {
  id: string;
  username: string;
  displayName: string;
  status?: string;
  avatarID?: string;
  online?: boolean;
}

/** Raw settings-page view of the caller's own profile (displayName may
 *  be empty here even though summaries fall back to the username). */
export interface SettingsProfile {
  id: string;
  username: string;
  displayName: string;
  status: string;
  avatarID: string;
  createdAt: number;
}

export interface Conversation {
  id: string;
  type: "dm" | "group";
  name?: string;
  creatorID: string;
  createdAt: number;
  lastActivityTS: number;
  lastPreview?: string;
  lastSenderID?: string;
  memberCount: number;
  peerID?: string;
  peerName?: string;
  peerOnline?: boolean;
  peerLastSeen?: number;
  peerAvatarID?: string;
  peerStatus?: string;
  displayName?: string;
  lastSeq: number;
  myReadSeq: number;
  unread: number;
}

/** Per-user delivery/read watermarks in a conversation. */
export interface ReadState {
  deliveredSeq: number;
  readSeq: number;
}

export interface Receipt extends ReadState {
  userID: string;
}

export interface SyncChange {
  conversation: Conversation;
  messages: Message[];
  hasMore: boolean;
  latestSeq: number;
}

/** Render metadata for one attachment (bytes live at /api/attachments/{id}). */
export interface AttachmentRef {
  id: string;
  filename: string;
  mimeType: string;
  size: number;
}

/** One emoji's aggregate on a message (reacted = this viewer). */
export interface ReactionAgg {
  emoji: string;
  count: number;
  reacted?: boolean;
}

export interface Message {
  convID: string;
  seq: number;
  senderID: string;
  body: string;
  ts: number;
  attachments?: AttachmentRef[];
  reactions?: ReactionAgg[];
}

export interface HistoryPage {
  messages: Message[];
  hasMore: boolean;
  oldestSeq: number;
}

export interface Session {
  token: string;
  user: UserSummary;
}

/** WebSocket frames (both directions). */
export interface Frame {
  type:
    | "send"
    | "typing"
    | "ping"
    | "resume"
    | "read"
    | "delivered"
    | "ack"
    | "message"
    | "presence"
    | "receipt"
    | "error"
    | "pong"
    | "reaction_add"
    | "reaction_remove"
    | "reaction_update";
  tempID?: string;
  convID?: string;
  seq?: number;
  senderID?: string;
  body?: string;
  userID?: string;
  online?: boolean;
  ts?: number;
  code?: string;
  message?: string;
  lastSeen?: Record<string, number>;
  deliveredSeq?: number;
  readSeq?: number;
  attachmentIDs?: string[]; // client → server on send
  attachments?: AttachmentRef[]; // server → client on ack/message
  emoji?: string;
  count?: number;
  added?: boolean;
}
