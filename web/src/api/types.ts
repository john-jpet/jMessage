export interface UserSummary {
  id: string;
  username: string;
  displayName: string;
  online?: boolean;
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

export interface Message {
  convID: string;
  seq: number;
  senderID: string;
  body: string;
  ts: number;
  attachments?: AttachmentRef[];
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
    | "pong";
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
}
