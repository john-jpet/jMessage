import Dexie, { type Table } from "dexie";
import type { Message } from "../api/types";

/** An unacked outgoing message. Persisted BEFORE the send is attempted,
 *  so it survives page refreshes and network loss; deleted on ack. Its
 *  tempID is the durable idempotency key — retries can never duplicate. */
export interface OutboxMessage {
  tempID: string;
  convID: string;
  body: string;
  ts: number;
  state: "pending" | "failed";
  /** Local attachment IDs to upload before this message can transmit. */
  attachmentLocalIDs?: string[];
}

/** A file picked by the user. The Blob itself lives in IndexedDB until
 *  the upload succeeds — offline-composed attachments survive refreshes
 *  exactly like outbox text does. */
export interface LocalAttachment {
  localID: string;
  serverID?: string;
  filename: string;
  mime: string;
  size: number;
  state: "local" | "uploading" | "uploaded" | "failed";
  blob?: Blob;
}

/** Highest contiguous-tip seq we hold per conversation; what we tell the
 *  server on resume/sync ("I have through N, give me the rest"). */
export interface SyncState {
  convID: string;
  lastSeq: number;
}

class LocalDB extends Dexie {
  messages!: Table<Message, [string, number]>;
  outbox!: Table<OutboxMessage, string>;
  syncState!: Table<SyncState, string>;
  attachments!: Table<LocalAttachment, string>;

  constructor() {
    super("jmessage");
    this.version(1).stores({
      // [convID+seq] as primary key makes message dedup free: replays,
      // resume overlaps, and sync overlaps all collapse via put().
      messages: "[convID+seq], convID",
      outbox: "tempID, convID, ts",
      syncState: "convID",
    });
    // Tier 2: locally-composed attachments (blob kept until uploaded).
    this.version(2).stores({
      attachments: "localID",
    });
  }
}

export const db = new LocalDB();

const OWNER_KEY = "jmessage.dbOwner";

/** ensureLocalOwner wipes the local cache when a different account logs
 *  in on this browser — cached conversations must not leak across users. */
export async function ensureLocalOwner(userID: string): Promise<void> {
  if (localStorage.getItem(OWNER_KEY) === userID) return;
  await Promise.all([
    db.messages.clear(),
    db.outbox.clear(),
    db.syncState.clear(),
    db.attachments.clear(),
  ]);
  localStorage.setItem(OWNER_KEY, userID);
}

/** storeMessage upserts one message and advances the conversation's
 *  sync watermark. */
export async function storeMessage(m: Message): Promise<void> {
  await db.transaction("rw", db.messages, db.syncState, async () => {
    await db.messages.put(m);
    await bumpSyncState(m.convID, m.seq);
  });
}

/** storeMessages bulk-upserts a batch (sync responses, history pages). */
export async function storeMessages(convID: string, msgs: Message[]): Promise<void> {
  if (msgs.length === 0) return;
  const top = Math.max(...msgs.map((m) => m.seq));
  await db.transaction("rw", db.messages, db.syncState, async () => {
    await db.messages.bulkPut(msgs);
    await bumpSyncState(convID, top);
  });
}

async function bumpSyncState(convID: string, seq: number): Promise<void> {
  const ss = await db.syncState.get(convID);
  if (!ss || seq > ss.lastSeq) {
    await db.syncState.put({ convID, lastSeq: seq });
  }
}

/** lastSeenMap builds the resume/sync request payload. */
export async function lastSeenMap(): Promise<Record<string, number>> {
  const rows = await db.syncState.toArray();
  return Object.fromEntries(rows.map((r) => [r.convID, r.lastSeq]));
}

const DEVICE_KEY = "jmessage.device";

/** deviceID returns this browser's stable device identifier. */
export function deviceID(): string {
  let id = localStorage.getItem(DEVICE_KEY);
  if (!id) {
    id = "dev-" + crypto.randomUUID();
    localStorage.setItem(DEVICE_KEY, id);
  }
  return id;
}
