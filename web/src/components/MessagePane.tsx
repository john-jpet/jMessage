import { useEffect, useLayoutEffect, useRef, useState, type UIEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { useLiveQuery } from "dexie-react-hooks";
import { api, attachmentURL } from "../api/client";
import type { AttachmentRef, HistoryPage, ReadState, Receipt } from "../api/types";
import { db, storeMessages, type LocalAttachment } from "../db/local";
import { formatSize } from "./Composer";

interface Props {
  convID: string;
  selfID: string;
  peerID?: string; // dm only: enables ✓✓ read receipts
  typing: Set<string>;
  liveReceipts: Map<string, ReadState>;
  onRead: (seq: number) => void;
  onRetry: (tempID: string) => void;
}

/**
 * MessagePane renders straight from IndexedDB via liveQuery: the
 * WebSocket layer, sync engine, and history fetches all write to Dexie,
 * and the pane updates reactively. Dedup is the [convID+seq] primary
 * key, so replays and overlaps never double-render.
 */
export default function MessagePane({
  convID,
  selfID,
  peerID,
  typing,
  liveReceipts,
  onRead,
  onRetry,
}: Props) {
  const scrollerRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);
  const prevHeightRef = useRef(0);
  const [loadingOlder, setLoadingOlder] = useState(false);

  const messages =
    useLiveQuery(() => db.messages.where("convID").equals(convID).sortBy("seq"), [convID]) ?? [];
  const outbox =
    useLiveQuery(() => db.outbox.where("convID").equals(convID).sortBy("ts"), [convID]) ?? [];
  // Local attachments backing the pending rows (for uploading chips).
  const localAtts =
    useLiveQuery(async () => {
      const ids = outbox.flatMap((r) => r.attachmentLocalIDs ?? []);
      if (ids.length === 0) return new Map<string, LocalAttachment>();
      const rows = await db.attachments.bulkGet(ids);
      return new Map(rows.filter((r): r is LocalAttachment => !!r).map((r) => [r.localID, r]));
    }, [outbox]) ?? new Map<string, LocalAttachment>();

  // Hydration for ✓/✓✓: REST snapshot merged with live receipt frames.
  const { data: restReceipts } = useQuery({
    queryKey: ["receipts", convID],
    queryFn: () => api<Receipt[]>("GET", `/api/conversations/${convID}/receipts`),
    staleTime: 60_000,
  });
  const peerState = mergePeerState(peerID, restReceipts, liveReceipts);

  const oldestSeq = messages[0]?.seq ?? 0;
  const hasOlder = oldestSeq > 1; // dense seqs: seq 1 exists iff anything older does

  async function loadOlder() {
    if (loadingOlder || !hasOlder) return;
    setLoadingOlder(true);
    try {
      const page = await api<HistoryPage>(
        "GET",
        `/api/conversations/${convID}/messages?limit=50&before=${oldestSeq}`,
      );
      await storeMessages(convID, page.messages);
    } finally {
      setLoadingOlder(false);
    }
  }

  function onScroll(e: UIEvent<HTMLDivElement>) {
    const el = e.currentTarget;
    stickToBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
    if (el.scrollTop < 40 && hasOlder && !loadingOlder) {
      prevHeightRef.current = el.scrollHeight;
      void loadOlder();
    }
  }

  // Keep the viewport anchored when older messages prepend.
  useLayoutEffect(() => {
    const el = scrollerRef.current;
    if (!el || prevHeightRef.current === 0) return;
    el.scrollTop += el.scrollHeight - prevHeightRef.current;
    prevHeightRef.current = 0;
  }, [oldestSeq]);

  // Auto-scroll on new content while the user is at the bottom.
  useEffect(() => {
    const el = scrollerRef.current;
    if (el && stickToBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [messages.length, outbox.length]);

  // Jump to bottom when switching conversations.
  useEffect(() => {
    stickToBottomRef.current = true;
    const el = scrollerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [convID]);

  // Report the read position (throttled inside the hook).
  const tailSeq = messages.length > 0 ? messages[messages.length - 1].seq : 0;
  useEffect(() => {
    if (tailSeq > 0) onRead(tailSeq);
  }, [tailSeq, onRead]);

  return (
    <div ref={scrollerRef} onScroll={onScroll} className="flex-1 overflow-y-auto p-4">
      {loadingOlder && <p className="mb-2 text-center text-xs text-slate-400">Loading older…</p>}
      {messages.map((m) => (
        <Bubble
          key={m.seq}
          mine={m.senderID === selfID}
          body={m.body}
          meta={`#${m.seq} · ${new Date(m.ts).toLocaleTimeString()}`}
          ticks={m.senderID === selfID ? tickState(m.seq, peerState) : undefined}
          attachments={m.attachments}
        />
      ))}
      {outbox.map((p) => (
        <Bubble
          key={p.tempID}
          mine
          body={p.body}
          meta={p.state === "failed" ? "failed — will retry on reconnect" : "sending…"}
          failed={p.state === "failed"}
          onRetry={p.state === "failed" ? () => onRetry(p.tempID) : undefined}
          pendingAtts={(p.attachmentLocalIDs ?? [])
            .map((id) => localAtts.get(id))
            .filter((a): a is LocalAttachment => !!a)}
        />
      ))}
      {typing.size > 0 && (
        <p className="mt-1 text-xs italic text-slate-400">
          {typing.size === 1 ? "Someone is typing…" : `${typing.size} people are typing…`}
        </p>
      )}
    </div>
  );
}

/** Effective peer watermark = max(REST snapshot, live frames). */
function mergePeerState(
  peerID: string | undefined,
  rest: Receipt[] | undefined,
  live: Map<string, ReadState>,
): ReadState | undefined {
  if (!peerID) return undefined;
  const snapshot = rest?.find((r) => r.userID === peerID);
  const fresh = live.get(peerID);
  if (!snapshot && !fresh) return undefined;
  return {
    deliveredSeq: Math.max(snapshot?.deliveredSeq ?? 0, fresh?.deliveredSeq ?? 0),
    readSeq: Math.max(snapshot?.readSeq ?? 0, fresh?.readSeq ?? 0),
  };
}

type Ticks = "sent" | "delivered" | "read";

function tickState(seq: number, peer: ReadState | undefined): Ticks {
  if (peer && peer.readSeq >= seq) return "read";
  if (peer && peer.deliveredSeq >= seq) return "delivered";
  return "sent";
}

function Bubble({
  mine,
  body,
  meta,
  failed,
  ticks,
  onRetry,
  attachments,
  pendingAtts,
}: {
  mine: boolean;
  body: string;
  meta: string;
  failed?: boolean;
  ticks?: Ticks;
  onRetry?: () => void;
  attachments?: AttachmentRef[];
  pendingAtts?: LocalAttachment[];
}) {
  return (
    <div className={`mb-2 flex ${mine ? "justify-end" : "justify-start"}`}>
      <div
        className={`max-w-[70%] rounded-lg px-3 py-2 text-sm ${
          failed
            ? "bg-red-100 text-red-800"
            : mine
              ? "bg-blue-600 text-white"
              : "bg-white text-slate-800 shadow-sm"
        }`}
      >
        {attachments?.map((a) =>
          a.mimeType.startsWith("image/") ? (
            <a key={a.id} href={attachmentURL(a.id)} target="_blank" rel="noreferrer">
              <img
                src={attachmentURL(a.id)}
                alt={a.filename}
                loading="lazy"
                className="mb-1 max-h-64 max-w-full rounded"
              />
            </a>
          ) : (
            <a
              key={a.id}
              href={attachmentURL(a.id)}
              download={a.filename}
              className={`mb-1 block rounded px-2 py-1 text-xs underline ${
                mine ? "bg-blue-700 text-blue-100" : "bg-slate-100 text-slate-700"
              }`}
            >
              📎 {a.filename} ({formatSize(a.size)})
            </a>
          ),
        )}
        {pendingAtts?.map((a) => (
          <span
            key={a.localID}
            className="mb-1 block rounded bg-blue-700/60 px-2 py-1 text-xs text-blue-100"
          >
            📎 {a.filename} ({formatSize(a.size)}) —{" "}
            {a.state === "uploading" ? "uploading…" : a.state === "failed" ? "upload failed" : "queued"}
          </span>
        ))}
        {body && <p className="whitespace-pre-wrap break-words">{body}</p>}
        <p className={`mt-0.5 text-[10px] ${mine && !failed ? "text-blue-200" : "text-slate-400"}`}>
          {meta}
          {ticks && (
            <span className={ticks === "read" ? "ml-1 text-cyan-300" : "ml-1"}>
              {ticks === "sent" ? "✓" : "✓✓"}
            </span>
          )}
          {onRetry && (
            <button onClick={onRetry} className="ml-2 underline">
              retry now
            </button>
          )}
        </p>
      </div>
    </div>
  );
}
