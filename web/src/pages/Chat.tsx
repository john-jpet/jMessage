import { useEffect, useRef, useState, type DragEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, clearSession } from "../api/client";
import type { Conversation, UserSummary } from "../api/types";
import { ensureLocalOwner } from "../db/local";
import { lastSeenLabel } from "../lib/time";
import { useWebSocket } from "../ws/useWebSocket";
import Avatar from "../components/Avatar";
import ConversationList from "../components/ConversationList";
import MessagePane from "../components/MessagePane";
import Composer from "../components/Composer";
import NewConversationDialog from "../components/NewConversationDialog";
import ProfileDialog from "../components/ProfileDialog";
import QuickSwitcher from "../components/QuickSwitcher";
import ShortcutHelp from "../components/ShortcutHelp";

export default function Chat({
  self,
  onLogout,
  onOpenSettings,
}: {
  self: UserSummary;
  onLogout: () => void;
  onOpenSettings: () => void;
}) {
  const [selected, setSelected] = useState<string | null>(null);
  const [dialog, setDialog] = useState<"new" | "switcher" | "shortcuts" | null>(null);
  const [profileUser, setProfileUser] = useState<string | null>(null);
  const [draftFiles, setDraftFiles] = useState<File[]>([]);
  const [dragging, setDragging] = useState(false);
  const [focusNonce, setFocusNonce] = useState(0);
  const dragDepth = useRef(0);

  // The hook reads this to decide when a message deserves a notification.
  const activeConvRef = useRef<string | null>(null);
  activeConvRef.current = selected;

  const ws = useWebSocket(self.id, activeConvRef);

  // Wipe the local cache if a different account was here before.
  useEffect(() => {
    void ensureLocalOwner(self.id);
  }, [self.id]);

  const { data: conversations = [], isLoading: convsLoading } = useQuery({
    queryKey: ["conversations"],
    queryFn: () => api<Conversation[]>("GET", "/api/conversations"),
    refetchInterval: 30_000,
  });

  const current = conversations.find((c) => c.id === selected) ?? null;

  function selectConversation(id: string) {
    setSelected(id);
    setDraftFiles([]);
    // Refocus the composer even when re-selecting the open conversation
    // (e.g. jumping via Ctrl+K back to where you already are).
    setFocusNonce((n) => n + 1);
  }

  // Global shortcuts: Ctrl+K switcher, Ctrl+/ shortcut help.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setDialog((d) => (d === "switcher" ? null : "switcher"));
      } else if (e.ctrlKey && e.key === "/") {
        e.preventDefault();
        setDialog((d) => (d === "shortcuts" ? null : "shortcuts"));
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Drag-and-drop attachments anywhere over the chat pane.
  function onDragEnter(e: DragEvent) {
    if (!current || !e.dataTransfer.types.includes("Files")) return;
    e.preventDefault();
    dragDepth.current++;
    setDragging(true);
  }
  function onDragLeave(e: DragEvent) {
    e.preventDefault();
    if (--dragDepth.current <= 0) {
      dragDepth.current = 0;
      setDragging(false);
    }
  }
  function onDrop(e: DragEvent) {
    e.preventDefault();
    dragDepth.current = 0;
    setDragging(false);
    if (!current) return;
    const files = Array.from(e.dataTransfer.files);
    if (files.length > 0) setDraftFiles((prev) => [...prev, ...files].slice(0, 10));
  }

  const peerOnline = current?.type === "dm" && (ws.presence.get(current.peerID ?? "") ?? current.peerOnline);

  return (
    <div className="flex h-screen flex-col bg-slate-100 dark:bg-slate-950">
      {!ws.connected && (
        <div
          className="flex items-center justify-center gap-2 bg-amber-100 px-4 py-1.5 text-xs text-amber-800 dark:bg-amber-900/60 dark:text-amber-200"
          role="status"
        >
          <span className="inline-block h-2 w-2 rounded-full bg-amber-500 animate-pulse-soft" />
          Connection lost — reconnecting automatically. Messages you send are saved and will
          deliver on reconnect.
        </div>
      )}

      <div className="flex min-h-0 flex-1">
        {/* Sidebar: full-screen on mobile until a conversation is open. */}
        <aside
          className={`w-full flex-col border-r border-slate-200 bg-white md:flex md:w-72 dark:border-slate-700 dark:bg-slate-900 ${
            current ? "hidden" : "flex"
          }`}
        >
          <div className="flex items-center justify-between border-b border-slate-200 p-3 dark:border-slate-700">
            <button
              onClick={() => setProfileUser(self.id)}
              className="flex items-center gap-2 rounded"
              aria-label="Your profile"
            >
              <Avatar
                id={self.id}
                name={self.displayName}
                online={ws.connected}
                avatarID={self.avatarID}
              />
              <span className="text-sm font-semibold text-slate-800 dark:text-slate-100">
                {self.displayName}
              </span>
            </button>
            <span className="flex items-center gap-1">
              <button
                onClick={onOpenSettings}
                aria-label="Settings"
                title="Settings"
                className="rounded-lg p-1.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800 dark:hover:text-slate-300"
              >
                ⚙
              </button>
              <button
                onClick={() => {
                  clearSession();
                  onLogout();
                }}
                aria-label="Log out"
                title="Log out"
                className="rounded-lg p-1.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-red-600 dark:hover:bg-slate-800"
              >
                ⏻
              </button>
            </span>
          </div>
          <button
            onClick={() => setDialog("new")}
            className="m-3 rounded-lg bg-accent py-1.5 text-sm text-white transition-colors hover:bg-accent-hover"
          >
            + New conversation
          </button>
          <ConversationList
            conversations={conversations}
            loading={convsLoading}
            selected={selected}
            onSelect={selectConversation}
            onNew={() => setDialog("new")}
            presence={ws.presence}
            typing={ws.typing}
            selfID={self.id}
          />
        </aside>

        {/* Main pane: hidden on mobile while browsing the list. */}
        <main
          className={`relative min-w-0 flex-1 flex-col md:flex ${current ? "flex" : "hidden"}`}
          onDragEnter={onDragEnter}
          onDragOver={(e) => e.preventDefault()}
          onDragLeave={onDragLeave}
          onDrop={onDrop}
        >
          {current ? (
            <>
              <header className="flex items-center gap-3 border-b border-slate-200 bg-white px-4 py-2.5 dark:border-slate-700 dark:bg-slate-900">
                <button
                  onClick={() => setSelected(null)}
                  className="rounded-lg p-1 text-slate-400 hover:text-slate-600 md:hidden dark:hover:text-slate-200"
                  aria-label="Back to conversations"
                >
                  ←
                </button>
                <button
                  onClick={() => current.type === "dm" && current.peerID && setProfileUser(current.peerID)}
                  className="flex min-w-0 items-center gap-3 rounded text-left"
                  aria-label={current.type === "dm" ? "View profile" : undefined}
                  disabled={current.type !== "dm"}
                >
                  <Avatar
                    id={current.type === "dm" ? (current.peerID ?? current.id) : current.id}
                    name={current.displayName || current.name || "Conversation"}
                    online={current.type === "dm" ? !!peerOnline : undefined}
                    avatarID={current.type === "dm" ? current.peerAvatarID : undefined}
                  />
                  <span className="min-w-0">
                    <span className="block truncate font-semibold text-slate-800 dark:text-slate-100">
                      {current.displayName || current.name || "Conversation"}
                    </span>
                    <span className="block truncate text-xs text-slate-400">
                      {current.type === "group"
                        ? `${current.memberCount} members`
                        : (ws.typing.get(current.id)?.size ?? 0) > 0
                          ? "typing…"
                          : current.peerStatus
                            ? current.peerStatus
                            : peerOnline
                              ? "online"
                              : lastSeenLabel(current.peerLastSeen ?? 0)}
                    </span>
                  </span>
                </button>
              </header>
              <MessagePane
                convID={current.id}
                selfID={self.id}
                peerID={current.peerID}
                typing={ws.typing.get(current.id) ?? new Set()}
                liveReceipts={ws.receipts.get(current.id) ?? new Map()}
                onRead={(seq) => ws.sendRead(current.id, seq)}
                onRetry={ws.retry}
                onOpenProfile={setProfileUser}
                onReact={(seq, emoji, add) => ws.sendReaction(current.id, seq, emoji, add)}
              />
              <Composer
                files={draftFiles}
                onFilesChange={setDraftFiles}
                onSend={(body, files) => ws.sendMessage(current.id, body, files)}
                onTyping={() => ws.sendTyping(current.id)}
                focusKey={`${current.id}:${focusNonce}`}
              />
              {dragging && (
                <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center bg-accent/10 backdrop-blur-[1px]">
                  <p className="rounded-xl border-2 border-dashed border-accent bg-white/90 px-6 py-4 text-sm font-medium text-accent dark:bg-slate-900/90">
                    Drop files to attach
                  </p>
                </div>
              )}
            </>
          ) : (
            <div className="hidden flex-1 flex-col items-center justify-center gap-2 text-center md:flex">
              <span className="text-4xl" aria-hidden>
                💬
              </span>
              <p className="text-sm text-slate-400">
                Select a conversation, or press{" "}
                <kbd className="rounded border border-slate-300 px-1 text-[11px] dark:border-slate-600">
                  Ctrl+K
                </kbd>{" "}
                to jump.
              </p>
            </div>
          )}
        </main>
      </div>

      {dialog === "new" && (
        <NewConversationDialog
          onClose={() => setDialog(null)}
          onCreated={(id) => {
            setDialog(null);
            selectConversation(id);
          }}
        />
      )}
      {dialog === "shortcuts" && <ShortcutHelp onClose={() => setDialog(null)} />}
      {dialog === "switcher" && (
        <QuickSwitcher
          conversations={conversations}
          onSelect={selectConversation}
          onClose={() => setDialog(null)}
        />
      )}
      {profileUser && (
        <ProfileDialog
          userID={profileUser}
          livePresence={ws.presence.get(profileUser)}
          onOpenConversation={selectConversation}
          onClose={() => setProfileUser(null)}
        />
      )}
    </div>
  );
}
