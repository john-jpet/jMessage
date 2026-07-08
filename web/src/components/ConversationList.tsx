import type { Conversation } from "../api/types";

interface Props {
  conversations: Conversation[];
  selected: string | null;
  onSelect: (id: string) => void;
  presence: Map<string, boolean>;
  selfID: string;
}

export default function ConversationList({ conversations, selected, onSelect, presence }: Props) {
  return (
    <nav className="flex-1 overflow-y-auto">
      {conversations.length === 0 && (
        <p className="p-4 text-sm text-slate-400">No conversations yet.</p>
      )}
      {conversations.map((c) => {
        const online = c.type === "dm" && (presence.get(c.peerID ?? "") ?? c.peerOnline);
        return (
          <button
            key={c.id}
            onClick={() => onSelect(c.id)}
            className={`block w-full border-b border-slate-100 px-3 py-2 text-left hover:bg-slate-50 ${
              selected === c.id ? "bg-blue-50" : ""
            }`}
          >
            <div className="flex items-center gap-2">
              {c.type === "dm" && (
                <span
                  className={`inline-block h-2 w-2 shrink-0 rounded-full ${
                    online ? "bg-green-500" : "bg-slate-300"
                  }`}
                />
              )}
              <span className="truncate font-medium text-slate-800">
                {c.displayName || c.name || "Conversation"}
              </span>
              {c.type === "group" && (
                <span className="ml-auto shrink-0 text-xs text-slate-400">{c.memberCount}</span>
              )}
            </div>
            {c.lastPreview && (
              <p className="mt-0.5 truncate text-xs text-slate-500">{c.lastPreview}</p>
            )}
          </button>
        );
      })}
    </nav>
  );
}
