import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, getToken, uploadBlob } from "../api/client";
import type { Frame, Message, ReadState, SyncChange } from "../api/types";
import { db, deviceID, lastSeenMap, storeMessage, storeMessages } from "../db/local";

export interface WsApi {
  connected: boolean;
  /** convID -> userIDs currently typing */
  typing: Map<string, Set<string>>;
  /** userID -> live presence override (wins over REST snapshots) */
  presence: Map<string, boolean>;
  /** convID -> userID -> live watermark (merge with REST receipts) */
  receipts: Map<string, Map<string, ReadState>>;
  sendMessage: (convID: string, body: string, files?: File[]) => void;
  sendTyping: (convID: string) => void;
  sendRead: (convID: string, seq: number) => void;
  retry: (tempID: string) => void;
}

const ACK_TIMEOUT_MS = 10_000;

/**
 * useWebSocket is the client's reliability engine:
 *
 *  - Outgoing messages live in the IndexedDB outbox (written before any
 *    network attempt), so they survive refreshes and offline periods.
 *  - The outbox flushes FIFO with one send in flight at a time, awaiting
 *    each ack — the server therefore assigns seqs in client order.
 *  - tempID is the idempotency key: any retry acks the original seq.
 *  - On (re)connect: resume{lastSeen} for small gaps, POST /api/sync for
 *    authoritative catch-up, then flush. All paths are idempotent via
 *    the [convID+seq] primary key in IndexedDB.
 */
export function useWebSocket(selfID: string): WsApi {
  const queryClient = useQueryClient();
  const [connected, setConnected] = useState(false);
  const [typing, setTyping] = useState<Map<string, Set<string>>>(new Map());
  const [presence, setPresence] = useState<Map<string, boolean>>(new Map());
  const [receipts, setReceipts] = useState<Map<string, Map<string, ReadState>>>(new Map());

  const wsRef = useRef<WebSocket | null>(null);
  const backoffRef = useRef(1000);
  const flushingRef = useRef(false);
  const ackWaitersRef = useRef<Map<string, (ok: boolean) => void>>(new Map());
  const lastTypingSentRef = useRef<Map<string, number>>(new Map());
  const lastReadSentRef = useRef<Map<string, { seq: number; at: number }>>(new Map());
  const desiredReadRef = useRef<Map<string, number>>(new Map());
  const readTimerRef = useRef<Map<string, number>>(new Map());

  /** wsSend fires a frame if the socket is open; silently drops
   *  otherwise (typing/read/delivered are all resendable state). */
  const wsSend = useCallback((obj: object) => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(obj));
  }, []);

  const clearTypingFor = useCallback((convID: string, userID: string) => {
    setTyping((prev) => {
      const set = prev.get(convID);
      if (!set?.has(userID)) return prev;
      const next = new Map(prev);
      const s = new Set(set);
      s.delete(userID);
      next.set(convID, s);
      return next;
    });
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
            attachments: f.attachments,
          };
          void storeMessage(msg);
          // Confirm receipt so the sender's ✓✓ can light up.
          wsSend({ type: "delivered", convID: msg.convID, seq: msg.seq });
          queryClient.invalidateQueries({ queryKey: ["conversations"] });
          clearTypingFor(msg.convID, msg.senderID);
          break;
        }
        case "ack": {
          const waiter = f.tempID ? ackWaitersRef.current.get(f.tempID) : undefined;
          void (async () => {
            const row = f.tempID ? await db.outbox.get(f.tempID) : undefined;
            if (row) {
              await storeMessage({
                convID: row.convID,
                seq: f.seq!,
                senderID: selfID,
                body: row.body,
                ts: f.ts ?? row.ts,
                attachments: f.attachments,
              });
              await db.outbox.delete(row.tempID);
              if (row.attachmentLocalIDs) {
                await db.attachments.bulkDelete(row.attachmentLocalIDs);
              }
            }
            waiter?.(true);
            queryClient.invalidateQueries({ queryKey: ["conversations"] });
          })();
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
          setTimeout(() => clearTypingFor(convID, userID), 3000);
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
        case "receipt": {
          setReceipts((prev) => {
            const next = new Map(prev);
            const conv = new Map(next.get(f.convID!) ?? []);
            conv.set(f.userID!, {
              deliveredSeq: f.deliveredSeq ?? 0,
              readSeq: f.readSeq ?? 0,
            });
            next.set(f.convID!, conv);
            return next;
          });
          break;
        }
        case "error": {
          if (f.tempID) {
            void db.outbox.update(f.tempID, { state: "failed" });
            ackWaitersRef.current.get(f.tempID)?.(false);
          }
          break;
        }
      }
    },
    [queryClient, selfID, clearTypingFor, wsSend],
  );

  /** transmit sends one outbox row and resolves on ack (false on error
   *  frame or timeout). */
  const transmit = useCallback(
    (tempID: string, convID: string, body: string, attachmentIDs?: string[]) => {
      return new Promise<boolean>((resolve) => {
        const ws = wsRef.current;
        if (!ws || ws.readyState !== WebSocket.OPEN) {
          resolve(false);
          return;
        }
        const timer = window.setTimeout(() => {
          ackWaitersRef.current.delete(tempID);
          resolve(false);
        }, ACK_TIMEOUT_MS);
        ackWaitersRef.current.set(tempID, (ok) => {
          clearTimeout(timer);
          ackWaitersRef.current.delete(tempID);
          resolve(ok);
        });
        ws.send(JSON.stringify({ type: "send", tempID, convID, body, attachmentIDs }));
      });
    },
    [],
  );

  /** uploadRowAttachments ensures every local attachment of an outbox
   *  row has a server ID, uploading any that don't (sequentially).
   *  Returns the server IDs, or null if an upload failed. A crash after
   *  an upload but before the send re-uses the stored serverID — and a
   *  lost serverID merely re-uploads, orphaning a Pending record the
   *  server janitor reclaims. */
  const uploadRowAttachments = useCallback(async (localIDs: string[]) => {
    const serverIDs: string[] = [];
    for (const lid of localIDs) {
      const att = await db.attachments.get(lid);
      if (!att) return null; // row is unsendable without its file
      if (att.serverID) {
        serverIDs.push(att.serverID);
        continue;
      }
      if (!att.blob) return null;
      await db.attachments.update(lid, { state: "uploading" });
      try {
        const ref = await uploadBlob(att.blob, att.filename);
        // Blob cleared after upload: the server owns the bytes now.
        await db.attachments.update(lid, {
          serverID: ref.id,
          state: "uploaded",
          blob: undefined,
        });
        serverIDs.push(ref.id);
      } catch {
        await db.attachments.update(lid, { state: "failed" });
        return null;
      }
    }
    return serverIDs;
  }, []);

  /** flushOutbox drains pending rows FIFO, one in flight at a time,
   *  uploading each row's attachments before its send. Stopping on the
   *  first failure preserves per-conversation order. */
  const flushOutbox = useCallback(async () => {
    if (flushingRef.current) return;
    flushingRef.current = true;
    try {
      for (;;) {
        const rows = await db.outbox.orderBy("ts").toArray();
        const next = rows.find((r) => r.state === "pending");
        if (!next) return;

        let attachmentIDs: string[] | undefined;
        if (next.attachmentLocalIDs?.length) {
          const ids = await uploadRowAttachments(next.attachmentLocalIDs);
          if (!ids) {
            await db.outbox.update(next.tempID, { state: "failed" });
            return;
          }
          attachmentIDs = ids;
        }

        const ok = await transmit(next.tempID, next.convID, next.body, attachmentIDs);
        if (!ok) {
          const still = await db.outbox.get(next.tempID);
          if (still) await db.outbox.update(next.tempID, { state: "failed" });
          return;
        }
      }
    } finally {
      flushingRef.current = false;
    }
  }, [transmit, uploadRowAttachments]);

  /** runSync pulls everything missing via REST and stores it locally. */
  const runSync = useCallback(async () => {
    try {
      const lastSeen = await lastSeenMap();
      const conversations = Object.entries(lastSeen).map(([id, lastSeq]) => ({ id, lastSeq }));
      const resp = await api<{ changes: SyncChange[] }>("POST", "/api/sync", { conversations });
      for (const ch of resp.changes) {
        await storeMessages(ch.conversation.id, ch.messages);
      }
      if (resp.changes.length > 0) {
        queryClient.invalidateQueries({ queryKey: ["conversations"] });
      }
      // A capped batch means more remains: repeat until caught up.
      if (resp.changes.some((c) => c.hasMore)) {
        void runSync();
      }
    } catch {
      /* sync retries on the next reconnect */
    }
  }, [queryClient]);

  useEffect(() => {
    let closed = false;
    let timer: number | undefined;

    const connect = () => {
      const token = getToken();
      if (!token || closed) return;
      const proto = location.protocol === "https:" ? "wss" : "ws";
      const ws = new WebSocket(
        `${proto}://${location.host}/ws?token=${encodeURIComponent(token)}&device=${deviceID()}`,
      );
      wsRef.current = ws;

      ws.onopen = () => {
        if (wsRef.current !== ws) return; // superseded (StrictMode remount)
        setConnected(true);
        backoffRef.current = 1000;
        void (async () => {
          try {
            // Small gaps replay over the socket; sync is authoritative.
            ws.send(JSON.stringify({ type: "resume", lastSeen: await lastSeenMap() }));
            await runSync();
            // Un-fail anything stranded by the disconnect, then flush.
            // (filter, not where(): "state" is not an indexed field)
            await db.outbox
              .toCollection()
              .modify((o) => {
                if (o.state === "failed") o.state = "pending";
              });
            await flushOutbox();
          } catch (err) {
            console.error("reconnect catch-up failed", err);
          }
        })();
        queryClient.invalidateQueries({ queryKey: ["conversations"] });
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
  }, [handleFrame, queryClient, runSync, flushOutbox]);

  const sendMessage = useCallback(
    (convID: string, body: string, files?: File[]) => {
      void (async () => {
        // Files land in IndexedDB first — an offline-composed attachment
        // survives refreshes exactly like outbox text does.
        const localIDs: string[] = [];
        for (const f of files ?? []) {
          const localID = crypto.randomUUID();
          await db.attachments.put({
            localID,
            filename: f.name || "file",
            mime: f.type || "application/octet-stream",
            size: f.size,
            state: "local",
            blob: f,
          });
          localIDs.push(localID);
        }
        // Durable before any network attempt: survives refresh/offline.
        await db.outbox.put({
          tempID: crypto.randomUUID(),
          convID,
          body,
          ts: Date.now(),
          state: "pending",
          attachmentLocalIDs: localIDs.length > 0 ? localIDs : undefined,
        });
        void flushOutbox();
      })();
    },
    [flushOutbox],
  );

  const retry = useCallback(
    (tempID: string) => {
      void (async () => {
        await db.outbox.update(tempID, { state: "pending" });
        void flushOutbox();
      })();
    },
    [flushOutbox],
  );

  const sendTyping = useCallback(
    (convID: string) => {
      const last = lastTypingSentRef.current.get(convID) ?? 0;
      if (Date.now() - last < 2000) return;
      lastTypingSentRef.current.set(convID, Date.now());
      wsSend({ type: "typing", convID });
    },
    [wsSend],
  );

  const fireRead = useCallback(
    (convID: string) => {
      const seq = desiredReadRef.current.get(convID) ?? 0;
      const prev = lastReadSentRef.current.get(convID);
      if (prev && seq <= prev.seq) return;
      lastReadSentRef.current.set(convID, { seq, at: Date.now() });
      wsSend({ type: "read", convID, seq });
      // Optimistically clear the unread badge.
      queryClient.setQueryData(["conversations"], (data: unknown) => {
        if (!Array.isArray(data)) return data;
        return data.map((c) =>
          c.id === convID ? { ...c, myReadSeq: seq, unread: Math.max(0, c.lastSeq - seq) } : c,
        );
      });
    },
    [queryClient, wsSend],
  );

  // Trailing-edge throttle (1/s per conversation): a suppressed report
  // is deferred, never dropped — the newest position always lands.
  const sendRead = useCallback(
    (convID: string, seq: number) => {
      const cur = desiredReadRef.current.get(convID) ?? 0;
      desiredReadRef.current.set(convID, Math.max(cur, seq));
      const prev = lastReadSentRef.current.get(convID);
      const since = Date.now() - (prev?.at ?? 0);
      if (!prev || since >= 1000) {
        fireRead(convID);
        return;
      }
      if (!readTimerRef.current.has(convID)) {
        readTimerRef.current.set(
          convID,
          window.setTimeout(() => {
            readTimerRef.current.delete(convID);
            fireRead(convID);
          }, 1000 - since),
        );
      }
    },
    [fireRead],
  );

  return { connected, typing, presence, receipts, sendMessage, sendTyping, sendRead, retry };
}
