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
  displayName?: string;
}

export interface Message {
  convID: string;
  seq: number;
  senderID: string;
  body: string;
  ts: number;
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
  type: "send" | "typing" | "ping" | "ack" | "message" | "presence" | "error" | "pong";
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
}
