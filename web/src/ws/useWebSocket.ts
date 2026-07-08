import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { getToken } from "../api/client";
import type { Frame, HistoryPage, Message } from "../api/types";

export interface PendingMessage {
  tempID: string;
  convID: string;
  body: string;
  ts: number;
  failed?: boolean;
}

export interface WsApi {
  connected: boolean;
  /** convID -> pending (optimistic) messages of this client */
  pending: Map<string, PendingMessage[]>;
  /** convID -> userIDs currently typing */
  typing: Map<string, Set<string>>;
  /** userID -> live presence override (wins over REST snapshots) */
  presence: Map<string, boolean>;
  sendMessage: (convID: string, body: string) => void;
  sendTyping: (convID: string) => void;
  retry: (tempID: string) => void;
}

const ACK_TIMEOUT_MS = 10_000;

/** appendToCache inserts a live message into the infinite-query cache
 *  (pages[0] is the newest page), deduping by seq. */
function appendToCache(
  data: InfiniteData<HistoryPage> | undefined,
  msg: Message,
): InfiniteData<HistoryPage> | undefined {
  if (!data || data.pages.length === 0) return data;
  const newest = data.pages[0];
  if (newest.messages.some((m) => m.seq === msg.seq)) return data;
  return {
    ...data,
    pages: [{ ...newest, messages: [...newest.messages, msg] }, ...data.pages.slice(1)],
  };
}

export function useWebSocket(selfID: string): WsApi {
  const queryClient = useQueryClient();
  const [connected, setConnected] = useState(false);
  const [pending, setPending] = useState<Map<string, PendingMessage[]>>(new Map());
  const [typing, setTyping] = useState<Map<string, Set<string>>>(new Map());
  const [presence, setPresence] = useState<Map<string, boolean>>(new Map());

  const wsRef = useRef<WebSocket | null>(null);
  const backoffRef = useRef(1000);
  const pendingRef = useRef<Map<string, PendingMessage>>(new Map()); // tempID -> msg
  const lastTypingSentRef = useRef<Map<string, number>>(new Map());

  const syncPending = useCallback(() => {
    const byConv = new Map<string, PendingMessage[]>();
    for (const p of pendingRef.current.values()) {
      const list = byConv.get(p.convID) ?? [];
      list.push(p);
      byConv.set(p.convID, list);
    }
    for (const list of byConv.values()) list.sort((a, b) => a.ts - b.ts);
    setPending(byConv);
  }, []);

  const handleFrame = useCallback(
    (f: Frame) => {
      switch (f.type) {
        case "message": {
          const msg: Message = {
            convID: f.convID!,
            seq: f.seq!,
            senderID: f.senderID!,
            body: f.body ?? "",
            ts: f.ts ?? Date.now(),
          };
          queryClient.setQueryData<InfiniteData<HistoryPage>>(
            ["messages", msg.convID],
            (data) => appendToCache(data, msg),
          );
          queryClient.invalidateQueries({ queryKey: ["conversations"] });
          // A real message clears that sender's typing indicator.
          setTyping((prev) => {
            const set = prev.get(msg.convID);
            if (!set?.has(msg.senderID)) return prev;
            const next = new Map(prev);
            const s = new Set(set);
            s.delete(msg.senderID);
            next.set(msg.convID, s);
            return next;
          });
          break;
        }
        case "ack": {
          const p = f.tempID ? pendingRef.current.get(f.tempID) : undefined;
          if (p) {
            pendingRef.current.delete(f.tempID!);
            syncPending();
            const msg: Message = {
              convID: p.convID,
              seq: f.seq!,
              senderID: selfID,
              body: p.body,
              ts: f.ts ?? p.ts,
            };
            queryClient.setQueryData<InfiniteData<HistoryPage>>(
              ["messages", p.convID],
              (data) => appendToCache(data, msg),
            );
            queryClient.invalidateQueries({ queryKey: ["conversations"] });
          }
          break;
        }
        case "typing": {
          const convID = f.convID!;
          const userID = f.userID!;
          setTyping((prev) => {
            const next = new Map(prev);
            const s = new Set(next.get(convID) ?? []);
            s.add(userID);
            next.set(convID, s);
            return next;
          });
          setTimeout(() => {
            setTyping((prev) => {
              const set = prev.get(convID);
              if (!set?.has(userID)) return prev;
              const next = new Map(prev);
              const s = new Set(set);
              s.delete(userID);
              next.set(convID, s);
              return next;
            });
          }, 3000);
          break;
        }
        case "presence": {
          setPresence((prev) => {
            const next = new Map(prev);
            next.set(f.userID!, !!f.online);
            return next;
          });
          break;
        }
        case "error": {
          if (f.tempID && pendingRef.current.has(f.tempID)) {
            pendingRef.current.get(f.tempID)!.failed = true;
            syncPending();
          }
          break;
        }
      }
    },
    [queryClient, selfID, syncPending],
  );

  useEffect(() => {
    let closed = false;
    let timer: number | undefined;

    const connect = () => {
      const token = getToken();
      if (!token || closed) return;
      const proto = location.protocol === "https:" ? "wss" : "ws";
      const ws = new WebSocket(`${proto}://${location.host}/ws?token=${encodeURIComponent(token)}`);
      wsRef.current = ws;

      ws.onopen = () => {
        if (wsRef.current !== ws) return; // superseded (StrictMode remount)
        setConnected(true);
        backoffRef.current = 1000;
        // Catch up on anything missed while disconnected.
        queryClient.invalidateQueries({ queryKey: ["conversations"] });
        queryClient.invalidateQueries({ queryKey: ["messages"] });
      };
      ws.onmessage = (ev) => {
        try {
          handleFrame(JSON.parse(ev.data as string) as Frame);
        } catch {
          /* ignore malformed frame */
        }
      };
      ws.onclose = () => {
        // Only react if this socket is still the current one — a socket
        // closed by StrictMode cleanup must not null out its replacement.
        if (wsRef.current !== ws) return;
        setConnected(false);
        wsRef.current = null;
        if (!closed) {
          timer = window.setTimeout(connect, backoffRef.current);
          backoffRef.current = Math.min(backoffRef.current * 2, 30_000);
        }
      };
    };

    connect();
    return () => {
      closed = true;
      if (timer) clearTimeout(timer);
      wsRef.current?.close();
    };
  }, [handleFrame, queryClient]);

  const transmit = useCallback(
    (p: PendingMessage) => {
      wsRef.current?.send(
        JSON.stringify({ type: "send", tempID: p.tempID, convID: p.convID, body: p.body }),
      );
      window.setTimeout(() => {
        const still = pendingRef.current.get(p.tempID);
        if (still && !still.failed) {
          still.failed = true;
          syncPending();
        }
      }, ACK_TIMEOUT_MS);
    },
    [syncPending],
  );

  const sendMessage = useCallback(
    (convID: string, body: string) => {
      const p: PendingMessage = {
        tempID: crypto.randomUUID(),
        convID,
        body,
        ts: Date.now(),
      };
      pendingRef.current.set(p.tempID, p);
      syncPending();
      transmit(p);
    },
    [syncPending, transmit],
  );

  const retry = useCallback(
    (tempID: string) => {
      const p = pendingRef.current.get(tempID);
      if (!p) return;
      p.failed = false;
      p.ts = Date.now();
      syncPending();
      transmit(p);
    },
    [syncPending, transmit],
  );

  const sendTyping = useCallback((convID: string) => {
    const last = lastTypingSentRef.current.get(convID) ?? 0;
    if (Date.now() - last < 2000) return;
    lastTypingSentRef.current.set(convID, Date.now());
    wsRef.current?.send(JSON.stringify({ type: "typing", convID }));
  }, []);

  return { connected, pending, typing, presence, sendMessage, sendTyping, retry };
}
