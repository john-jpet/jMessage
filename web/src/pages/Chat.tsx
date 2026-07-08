import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, clearSession } from "../api/client";
import type { Conversation, UserSummary } from "../api/types";
import { useWebSocket } from "../ws/useWebSocket";
import ConversationList from "../components/ConversationList";
import MessagePane from "../components/MessagePane";
import Composer from "../components/Composer";
import NewConversationDialog from "../components/NewConversationDialog";

export default function Chat({ self, onLogout }: { self: UserSummary; onLogout: () => void }) {
  const [selected, setSelected] = useState<string | null>(null);
  const [showNew, setShowNew] = useState(false);
  const ws = useWebSocket(self.id);

  const { data: conversations = [] } = useQuery({
    queryKey: ["conversations"],
    queryFn: () => api<Conversation[]>("GET", "/api/conversations"),
    refetchInterval: 30_000,
  });

  const current = conversations.find((c) => c.id === selected) ?? null;

  return (
    <div className="flex h-screen bg-slate-100">
      <aside className="flex w-72 flex-col border-r border-slate-200 bg-white">
        <div className="flex items-center justify-between border-b border-slate-200 p-3">
          <div>
            <div className="font-semibold text-slate-800">{self.displayName}</div>
            <div className="flex items-center gap-1 text-xs text-slate-500">
              <span
                className={`inline-block h-2 w-2 rounded-full ${ws.connected ? "bg-green-500" : "bg-red-400"}`}
              />
              {ws.connected ? "connected" : "reconnecting…"}
            </div>
          </div>
          <button
            onClick={() => {
              clearSession();
              onLogout();
            }}
            className="text-xs text-slate-500 hover:text-red-600"
          >
            Logout
          </button>
        </div>
        <button
          onClick={() => setShowNew(true)}
          className="m-3 rounded bg-blue-600 py-1.5 text-sm text-white hover:bg-blue-700"
        >
          + New conversation
        </button>
        <ConversationList
          conversations={conversations}
          selected={selected}
          onSelect={setSelected}
          presence={ws.presence}
          selfID={self.id}
        />
      </aside>

      <main className="flex flex-1 flex-col">
        {current ? (
          <>
            <header className="border-b border-slate-200 bg-white px-4 py-3">
              <h2 className="font-semibold text-slate-800">
                {current.displayName || current.name || "Conversation"}
                {current.type === "dm" && (
                  <span
                    className={`ml-2 inline-block h-2 w-2 rounded-full ${
                      (ws.presence.get(current.peerID ?? "") ?? current.peerOnline)
                        ? "bg-green-500"
                        : "bg-slate-300"
                    }`}
                  />
                )}
              </h2>
              <p className="text-xs text-slate-500">
                {current.type === "group" ? `${current.memberCount} members` : "direct message"}
              </p>
            </header>
            <MessagePane
              convID={current.id}
              selfID={self.id}
              pending={ws.pending.get(current.id) ?? []}
              typing={ws.typing.get(current.id) ?? new Set()}
              onRetry={ws.retry}
            />
            <Composer
              onSend={(body) => ws.sendMessage(current.id, body)}
              onTyping={() => ws.sendTyping(current.id)}
            />
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center text-slate-400">
            Select or create a conversation
          </div>
        )}
      </main>

      {showNew && (
        <NewConversationDialog
          onClose={() => setShowNew(false)}
          onCreated={(id) => {
            setShowNew(false);
            setSelected(id);
          }}
        />
      )}
    </div>
  );
}
