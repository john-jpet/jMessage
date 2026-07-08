import { useEffect, useLayoutEffect, useRef, type UIEvent } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { HistoryPage } from "../api/types";
import type { PendingMessage } from "../ws/useWebSocket";

interface Props {
  convID: string;
  selfID: string;
  pending: PendingMessage[];
  typing: Set<string>;
  onRetry: (tempID: string) => void;
}

export default function MessagePane({ convID, selfID, pending, typing, onRetry }: Props) {
  const scrollerRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);
  const prevHeightRef = useRef(0);

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery({
    queryKey: ["messages", convID],
    queryFn: ({ pageParam }) =>
      api<HistoryPage>(
        "GET",
        `/api/conversations/${convID}/messages?limit=50${pageParam ? `&before=${pageParam}` : ""}`,
      ),
    initialPageParam: 0,
    getNextPageParam: (page) => (page.hasMore ? page.oldestSeq : undefined),
  });

  // pages[0] is the newest page; older pages follow. Render oldest-first.
  const messages = data ? [...data.pages].reverse().flatMap((p) => p.messages) : [];

  function onScroll(e: UIEvent<HTMLDivElement>) {
    const el = e.currentTarget;
    stickToBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
    if (el.scrollTop < 40 && hasNextPage && !isFetchingNextPage) {
      prevHeightRef.current = el.scrollHeight;
      fetchNextPage();
    }
  }

  // Keep the viewport anchored when older messages prepend.
  useLayoutEffect(() => {
    const el = scrollerRef.current;
    if (!el || prevHeightRef.current === 0) return;
    el.scrollTop += el.scrollHeight - prevHeightRef.current;
    prevHeightRef.current = 0;
  }, [data?.pages.length]);

  // Auto-scroll on new content while the user is at the bottom.
  useEffect(() => {
    const el = scrollerRef.current;
    if (el && stickToBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [messages.length, pending.length]);

  // Jump to bottom when switching conversations.
  useEffect(() => {
    stickToBottomRef.current = true;
    const el = scrollerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [convID]);

  return (
    <div ref={scrollerRef} onScroll={onScroll} className="flex-1 overflow-y-auto p-4">
      {isFetchingNextPage && (
        <p className="mb-2 text-center text-xs text-slate-400">Loading older…</p>
      )}
      {messages.map((m) => (
        <Bubble
          key={m.seq}
          mine={m.senderID === selfID}
          body={m.body}
          meta={`#${m.seq} · ${new Date(m.ts).toLocaleTimeString()}`}
        />
      ))}
      {pending.map((p) => (
        <Bubble
          key={p.tempID}
          mine
          body={p.body}
          meta={p.failed ? "failed" : "sending…"}
          failed={p.failed}
          onRetry={p.failed ? () => onRetry(p.tempID) : undefined}
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

function Bubble({
  mine,
  body,
  meta,
  failed,
  onRetry,
}: {
  mine: boolean;
  body: string;
  meta: string;
  failed?: boolean;
  onRetry?: () => void;
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
        <p className="whitespace-pre-wrap break-words">{body}</p>
        <p className={`mt-0.5 text-[10px] ${mine && !failed ? "text-blue-200" : "text-slate-400"}`}>
          {meta}
          {onRetry && (
            <button onClick={onRetry} className="ml-2 underline">
              retry
            </button>
          )}
        </p>
      </div>
    </div>
  );
}
