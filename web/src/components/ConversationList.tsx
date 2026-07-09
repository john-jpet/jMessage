import type { Conversation } from "../api/types";
import Avatar from "./Avatar";
import { relativeTime } from "../lib/time";

interface Props {
  conversations: Conversation[];
  loading: boolean;
  selected: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
  presence: Map<string, boolean>;
  typing: Map<string, Set<string>>;
  selfID: string;
}

export default function ConversationList({
  conversations,
  loading,
  selected,
  onSelect,
  onNew,
  presence,
  typing,
}: Props) {
  if (loading && conversations.length === 0) {
    return (
      <nav className="flex-1 overflow-y-auto" aria-label="Conversations" aria-busy="true">
        {[...Array(6)].map((_, i) => (
          <SkeletonRow key={i} />
        ))}
      </nav>
    );
  }

  if (conversations.length === 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
        <span className="text-4xl" aria-hidden>
          💬
        </span>
        <p className="text-sm font-medium text-slate-600 dark:text-slate-300">Welcome to jMessage</p>
        <p className="text-xs text-slate-400">
          Start a direct message or create a group to begin chatting.
        </p>
        <button
          onClick={onNew}
          className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white transition-colors hover:bg-accent-hover"
        >
          Start a conversation
        </button>
      </div>
    );
  }

  return (
    <nav className="flex-1 overflow-y-auto" aria-label="Conversations">
      {conversations.map((c) => {
        const online = c.type === "dm" && (presence.get(c.peerID ?? "") ?? c.peerOnline);
        const someoneTyping = (typing.get(c.id)?.size ?? 0) > 0;
        const name = c.displayName || c.name || "Conversation";
        const active = selected === c.id;
        return (
          <button
            key={c.id}
            onClick={() => onSelect(c.id)}
            aria-current={active ? "true" : undefined}
            className={`flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors ${
              active
                ? "bg-accent/10 dark:bg-accent/20"
                : "hover:bg-slate-50 dark:hover:bg-slate-800"
            }`}
          >
            <Avatar
              id={c.type === "dm" ? (c.peerID ?? c.id) : c.id}
              name={name}
              online={c.type === "dm" ? !!online : undefined}
            />
            <span className="min-w-0 flex-1">
              <span className="flex items-baseline justify-between gap-2">
                <span
                  className={`truncate text-sm ${
                    c.unread > 0
                      ? "font-semibold text-slate-900 dark:text-white"
                      : "font-medium text-slate-700 dark:text-slate-200"
                  }`}
                >
                  {name}
                </span>
                <span className="shrink-0 text-[10px] text-slate-400">
                  {relativeTime(c.lastActivityTS)}
                </span>
              </span>
              <span className="mt-0.5 flex items-center justify-between gap-2">
                {someoneTyping ? (
                  <span className="truncate text-xs italic text-accent">typing…</span>
                ) : (
                  <span
                    className={`truncate text-xs ${
                      c.unread > 0
                        ? "text-slate-600 dark:text-slate-300"
                        : "text-slate-400 dark:text-slate-500"
                    }`}
                  >
                    {c.lastPreview || (c.type === "group" ? `${c.memberCount} members` : "No messages yet")}
                  </span>
                )}
                {c.unread > 0 && (
                  <span
                    className="shrink-0 rounded-full bg-accent px-1.5 py-0.5 text-[10px] font-semibold text-white"
                    aria-label={`${c.unread} unread`}
                  >
                    {c.unread > 99 ? "99+" : c.unread}
                  </span>
                )}
              </span>
            </span>
          </button>
        );
      })}
    </nav>
  );
}

function SkeletonRow() {
  return (
    <div className="flex items-center gap-3 px-3 py-2.5 animate-pulse-soft">
      <span className="h-9 w-9 rounded-full bg-slate-200 dark:bg-slate-700" />
      <span className="flex-1 space-y-2">
        <span className="block h-3 w-2/5 rounded bg-slate-200 dark:bg-slate-700" />
        <span className="block h-2.5 w-4/5 rounded bg-slate-100 dark:bg-slate-800" />
      </span>
    </div>
  );
}
