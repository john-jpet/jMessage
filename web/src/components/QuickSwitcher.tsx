import { useEffect, useRef, useState } from "react";
import type { Conversation } from "../api/types";
import { useEscape } from "../lib/dialog";
import Avatar from "./Avatar";

interface Props {
  conversations: Conversation[];
  onSelect: (id: string) => void;
  onClose: () => void;
}

/** QuickSwitcher (Ctrl+K): type to filter, ↑/↓ to move, Enter to jump. */
export default function QuickSwitcher({ conversations, onSelect, onClose }: Props) {
  const [query, setQuery] = useState("");
  const [index, setIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  useEscape(onClose);

  useEffect(() => inputRef.current?.focus(), []);

  const q = query.trim().toLowerCase();
  const results = conversations.filter((c) =>
    (c.displayName || c.name || "").toLowerCase().includes(q),
  );
  const clamped = Math.min(index, Math.max(0, results.length - 1));

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setIndex((i) => Math.min(i + 1, results.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setIndex((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter" && results[clamped]) {
      e.preventDefault();
      onSelect(results[clamped].id);
      onClose();
    }
  }

  return (
    <div
      className="fixed inset-0 z-40 flex items-start justify-center bg-black/40 pt-[15vh]"
      onClick={onClose}
      role="dialog"
      aria-label="Quick conversation switcher"
    >
      <div
        className="w-[28rem] max-w-[calc(100vw-2rem)] overflow-hidden rounded-xl bg-white shadow-xl animate-fade-in dark:bg-slate-800"
        onClick={(e) => e.stopPropagation()}
      >
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setIndex(0);
          }}
          onKeyDown={onKeyDown}
          placeholder="Jump to a conversation…"
          aria-label="Search conversations"
          className="w-full border-b border-slate-200 bg-transparent px-4 py-3 text-sm text-slate-900 placeholder:text-slate-400 focus:outline-none dark:border-slate-700 dark:text-slate-100"
        />
        <ul className="max-h-72 overflow-y-auto py-1" role="listbox">
          {results.length === 0 && (
            <li className="px-4 py-6 text-center text-sm text-slate-400">
              No conversations match “{query}”.
              <br />
              <span className="text-xs">Try a different name, or press Escape to close.</span>
            </li>
          )}
          {results.map((c, i) => {
            const name = c.displayName || c.name || "Conversation";
            return (
              <li key={c.id} role="option" aria-selected={i === clamped}>
                <button
                  onClick={() => {
                    onSelect(c.id);
                    onClose();
                  }}
                  onMouseEnter={() => setIndex(i)}
                  className={`flex w-full items-center gap-3 px-4 py-2 text-left text-sm ${
                    i === clamped
                      ? "bg-accent/10 text-slate-900 dark:bg-accent/20 dark:text-white"
                      : "text-slate-700 dark:text-slate-300"
                  }`}
                >
                  <Avatar
                    id={c.type === "dm" ? (c.peerID ?? c.id) : c.id}
                    name={name}
                    size="sm"
                    avatarID={c.type === "dm" ? c.peerAvatarID : undefined}
                  />
                  <span className="flex-1 truncate">{name}</span>
                  {c.unread > 0 && (
                    <span className="rounded-full bg-accent px-1.5 text-[10px] font-semibold text-white">
                      {c.unread}
                    </span>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}
