import { useEffect, useLayoutEffect, useRef, useState, type UIEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { useLiveQuery } from "dexie-react-hooks";
import { api, attachmentURL } from "../api/client";
import type {
  AttachmentRef,
  HistoryPage,
  Message,
  ReadState,
  Receipt,
  UserSummary,
} from "../api/types";
import { db, storeMessages, type LocalAttachment } from "../db/local";
import { clockTime, dayLabel, isSameDay } from "../lib/time";
import Avatar from "./Avatar";
import { formatSize } from "./Composer";

interface Props {
  convID: string;
  selfID: string;
  peerID?: string; // dm only: enables ✓✓ read receipts
  typing: Set<string>;
  liveReceipts: Map<string, ReadState>;
  onRead: (seq: number) => void;
  onRetry: (tempID: string) => void;
  onOpenProfile: (userID: string) => void;
}

/** Messages closer together than this share one sender block. */
const GROUP_WINDOW_MS = 5 * 60_000;

/** Per-conversation scroll positions survive switches (module-level so
 *  they outlive the component). */
const scrollMemory = new Map<string, number>();

export default function MessagePane({
  convID,
  selfID,
  peerID,
  typing,
  liveReceipts,
  onRead,
  onRetry,
  onOpenProfile,
}: Props) {
  const scrollerRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);
  const prevHeightRef = useRef(0);
  const [loadingOlder, setLoadingOlder] = useState(false);

  // undefined = IndexedDB still answering (skeletons); [] = truly empty.
  const messagesRaw = useLiveQuery(
    () => db.messages.where("convID").equals(convID).sortBy("seq"),
    [convID],
  );
  const messages = messagesRaw ?? [];
  const initialLoading = messagesRaw === undefined;
  const outbox =
    useLiveQuery(() => db.outbox.where("convID").equals(convID).sortBy("ts"), [convID]) ?? [];
  const localAtts =
    useLiveQuery(async () => {
      const ids = outbox.flatMap((r) => r.attachmentLocalIDs ?? []);
      if (ids.length === 0) return new Map<string, LocalAttachment>();
      const rows = await db.attachments.bulkGet(ids);
      return new Map(rows.filter((r): r is LocalAttachment => !!r).map((r) => [r.localID, r]));
    }, [outbox]) ?? new Map<string, LocalAttachment>();

  // Member names/presence for group heads and profile clicks.
  const { data: members = [] } = useQuery({
    queryKey: ["members", convID],
    queryFn: () => api<UserSummary[]>("GET", `/api/conversations/${convID}/members`),
    staleTime: 60_000,
  });
  const nameOf = (uid: string) =>
    members.find((m) => m.id === uid)?.displayName ?? uid.replace(/^0+/, "user ");

  // Hydration for ✓/✓✓: REST snapshot merged with live receipt frames.
  const { data: restReceipts } = useQuery({
    queryKey: ["receipts", convID],
    queryFn: () => api<Receipt[]>("GET", `/api/conversations/${convID}/receipts`),
    staleTime: 60_000,
  });
  const peerState = mergePeerState(peerID, restReceipts, liveReceipts);

  const oldestSeq = messages[0]?.seq ?? 0;
  const hasOlder = oldestSeq > 1; // dense seqs: exact answer, no guessing

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
    scrollMemory.set(convID, el.scrollTop);
    // Prefetch well before the user reaches the top.
    if (el.scrollTop < 300 && hasOlder && !loadingOlder) {
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

  // Restore the remembered position when switching conversations
  // (bottom for first-time opens).
  useLayoutEffect(() => {
    const el = scrollerRef.current;
    if (!el) return;
    const saved = scrollMemory.get(convID);
    if (saved !== undefined) {
      el.scrollTop = saved;
      stickToBottomRef.current = el.scrollHeight - saved - el.clientHeight < 60;
    } else {
      el.scrollTop = el.scrollHeight;
      stickToBottomRef.current = true;
    }
  }, [convID, initialLoading]);

  // Report the read position (throttled inside the hook).
  const tailSeq = messages.length > 0 ? messages[messages.length - 1].seq : 0;
  useEffect(() => {
    if (tailSeq > 0) onRead(tailSeq);
  }, [tailSeq, onRead]);

  if (initialLoading) {
    return (
      <div className="flex-1 overflow-y-auto p-4" aria-busy="true">
        {[65, 40, 75, 30, 55].map((w, i) => (
          <div key={i} className={`mb-3 flex ${i % 2 ? "justify-end" : "justify-start"}`}>
            <span
              className="h-9 rounded-xl bg-slate-200 animate-pulse-soft dark:bg-slate-700"
              style={{ width: `${w}%`, maxWidth: "20rem" }}
            />
          </div>
        ))}
      </div>
    );
  }

  if (messages.length === 0 && outbox.length === 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 p-6 text-center">
        <span className="text-4xl" aria-hidden>
          👋
        </span>
        <p className="text-sm font-medium text-slate-600 dark:text-slate-300">No messages yet</p>
        <p className="text-xs text-slate-400">Say hello to start the conversation.</p>
      </div>
    );
  }

  return (
    <div ref={scrollerRef} onScroll={onScroll} className="flex-1 overflow-y-auto px-4 py-2">
      {loadingOlder && (
        <p className="mb-2 text-center text-xs text-slate-400 animate-pulse-soft">Loading older…</p>
      )}
      {messages.map((m, i) => {
        const prev = messages[i - 1];
        const newDay = !prev || !isSameDay(prev.ts, m.ts);
        const groupHead =
          newDay || !prev || prev.senderID !== m.senderID || m.ts - prev.ts > GROUP_WINDOW_MS;
        const mine = m.senderID === selfID;
        return (
          <div key={m.seq}>
            {newDay && <DaySeparator ts={m.ts} />}
            <MessageRow
              msg={m}
              mine={mine}
              groupHead={groupHead}
              senderName={nameOf(m.senderID)}
              ticks={mine ? tickState(m.seq, peerState) : undefined}
              onOpenProfile={onOpenProfile}
            />
          </div>
        );
      })}
      {outbox.map((p) => (
        <PendingRow
          key={p.tempID}
          body={p.body}
          failed={p.state === "failed"}
          onRetry={p.state === "failed" ? () => onRetry(p.tempID) : undefined}
          atts={(p.attachmentLocalIDs ?? [])
            .map((id) => localAtts.get(id))
            .filter((a): a is LocalAttachment => !!a)}
        />
      ))}
      {typing.size > 0 && (
        <p className="mt-1 text-xs italic text-slate-400 animate-fade-in" role="status">
          {typing.size === 1 ? "typing…" : `${typing.size} people are typing…`}
        </p>
      )}
    </div>
  );
}

function DaySeparator({ ts }: { ts: number }) {
  return (
    <div className="my-4 flex items-center gap-3" role="separator">
      <span className="h-px flex-1 bg-slate-200 dark:bg-slate-700" />
      <span className="text-[11px] font-medium text-slate-400">{dayLabel(ts)}</span>
      <span className="h-px flex-1 bg-slate-200 dark:bg-slate-700" />
    </div>
  );
}

function MessageRow({
  msg,
  mine,
  groupHead,
  senderName,
  ticks,
  onOpenProfile,
}: {
  msg: Message;
  mine: boolean;
  groupHead: boolean;
  senderName: string;
  ticks?: Ticks;
  onOpenProfile: (userID: string) => void;
}) {
  return (
    <div className={`flex gap-2 ${groupHead ? "mt-2.5" : "mt-0.5"} ${mine ? "justify-end" : ""}`}>
      {!mine && (
        <span className="w-7 shrink-0">
          {groupHead && (
            <button
              onClick={() => onOpenProfile(msg.senderID)}
              aria-label={`View ${senderName}'s profile`}
              className="rounded-full"
            >
              <Avatar id={msg.senderID} name={senderName} size="sm" />
            </button>
          )}
        </span>
      )}
      <div className={`max-w-[70%] ${mine ? "items-end" : ""}`}>
        {groupHead && !mine && (
          <p className="mb-0.5 flex items-baseline gap-2 text-xs">
            <button
              onClick={() => onOpenProfile(msg.senderID)}
              className="font-semibold text-slate-700 hover:underline dark:text-slate-200"
            >
              {senderName}
            </button>
            <span className="text-[10px] text-slate-400">{clockTime(msg.ts)}</span>
          </p>
        )}
        <div
          className={`rounded-[var(--radius-bubble)] px-3 py-1.5 text-sm animate-fade-in ${
            mine ? "bg-accent text-white" : "bg-white text-slate-800 shadow-sm dark:bg-slate-800 dark:text-slate-100"
          }`}
        >
          <Attachments atts={msg.attachments} mine={mine} />
          {msg.body && <p className="whitespace-pre-wrap break-words">{msg.body}</p>}
        </div>
        {/* Fixed-height meta line: state changes never shift layout. */}
        <p className={`h-4 text-[10px] leading-4 text-slate-400 ${mine ? "text-right" : ""}`}>
          {mine && groupHead && clockTime(msg.ts)}
          {ticks && (
            <span className={ticks === "read" ? "ml-1 font-bold text-accent" : "ml-1"}>
              {ticks === "sent" ? "✓" : "✓✓"}
            </span>
          )}
        </p>
      </div>
    </div>
  );
}

function PendingRow({
  body,
  failed,
  onRetry,
  atts,
}: {
  body: string;
  failed: boolean;
  onRetry?: () => void;
  atts: LocalAttachment[];
}) {
  return (
    <div className="mt-0.5 flex justify-end">
      <div className="max-w-[70%]">
        <div
          className={`rounded-[var(--radius-bubble)] px-3 py-1.5 text-sm ${
            failed ? "bg-red-100 text-red-800 dark:bg-red-900/50 dark:text-red-200" : "bg-accent/70 text-white"
          }`}
        >
          {atts.map((a) => (
            <span key={a.localID} className="mb-1 block rounded bg-black/10 px-2 py-1 text-xs">
              📎 {a.filename} ({formatSize(a.size)}) —{" "}
              {a.state === "uploading" ? "uploading…" : a.state === "failed" ? "upload failed" : "queued"}
            </span>
          ))}
          {body && <p className="whitespace-pre-wrap break-words">{body}</p>}
        </div>
        <p className="h-4 text-right text-[10px] leading-4 text-slate-400" role="status">
          {failed ? (
            <>
              failed — retries on reconnect
              {onRetry && (
                <button onClick={onRetry} className="ml-1.5 text-accent underline">
                  retry now
                </button>
              )}
            </>
          ) : (
            <span className="animate-pulse-soft">sending…</span>
          )}
        </p>
      </div>
    </div>
  );
}

function Attachments({ atts, mine }: { atts?: AttachmentRef[]; mine: boolean }) {
  if (!atts?.length) return null;
  return (
    <>
      {atts.map((a) =>
        a.mimeType.startsWith("image/") ? (
          <a key={a.id} href={attachmentURL(a.id)} target="_blank" rel="noreferrer">
            <img
              src={attachmentURL(a.id)}
              alt={a.filename}
              loading="lazy"
              className="mb-1 max-h-64 max-w-full rounded-lg"
            />
          </a>
        ) : (
          <a
            key={a.id}
            href={attachmentURL(a.id)}
            download={a.filename}
            className={`mb-1 block rounded px-2 py-1 text-xs underline ${
              mine ? "bg-black/15 text-white" : "bg-slate-100 text-slate-700 dark:bg-slate-700 dark:text-slate-200"
            }`}
          >
            📎 {a.filename} ({formatSize(a.size)})
          </a>
        ),
      )}
    </>
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
