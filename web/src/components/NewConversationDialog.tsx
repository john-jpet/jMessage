import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Conversation, UserSummary } from "../api/types";
import { useEscape } from "../lib/dialog";

interface Props {
  onClose: () => void;
  onCreated: (convID: string) => void;
}

export default function NewConversationDialog({ onClose, onCreated }: Props) {
  useEscape(onClose);
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<"dm" | "group">("dm");
  const [usernames, setUsernames] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function create() {
    setError("");
    setBusy(true);
    try {
      const names = usernames
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      if (names.length === 0) throw new Error("Enter at least one username.");
      if (mode === "dm" && names.length !== 1) throw new Error("A DM takes exactly one username.");

      const ids: string[] = [];
      for (const n of names) {
        const u = await api<UserSummary>("GET", `/api/users/lookup?username=${encodeURIComponent(n)}`);
        ids.push(u.id);
      }
      const conv = await api<Conversation>("POST", "/api/conversations", {
        type: mode,
        name: mode === "group" ? name : undefined,
        participantIDs: ids,
      });
      await queryClient.invalidateQueries({ queryKey: ["conversations"] });
      onCreated(conv.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong — please try again.");
    } finally {
      setBusy(false);
    }
  }

  const inputCls =
    "mt-1 w-full rounded-lg border border-slate-300 bg-white px-2 py-1.5 text-slate-900 focus:border-accent focus:outline-none dark:border-slate-600 dark:bg-slate-700 dark:text-slate-100";

  return (
    <div
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
      role="dialog"
      aria-label="New conversation"
    >
      <div
        className="w-96 max-w-full rounded-xl bg-white p-5 shadow-xl animate-fade-in dark:bg-slate-800 dark:text-slate-100"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="mb-3 font-semibold text-slate-800 dark:text-slate-100">New conversation</h3>

        <div className="mb-3 flex gap-2 text-sm">
          {(["dm", "group"] as const).map((m) => (
            <button
              key={m}
              onClick={() => setMode(m)}
              className={`rounded-full px-3 py-1 transition-colors ${
                mode === m
                  ? "bg-accent text-white"
                  : "bg-slate-100 text-slate-600 hover:bg-slate-200 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600"
              }`}
            >
              {m === "dm" ? "Direct message" : "Group"}
            </button>
          ))}
        </div>

        {mode === "group" && (
          <label className="mb-2 block text-sm">
            <span className="text-slate-600 dark:text-slate-300">Group name</span>
            <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} />
          </label>
        )}
        <label className="mb-3 block text-sm">
          <span className="text-slate-600 dark:text-slate-300">
            {mode === "dm" ? "Username" : "Usernames (comma-separated)"}
          </span>
          <input
            className={inputCls}
            value={usernames}
            onChange={(e) => setUsernames(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && create()}
            autoFocus
          />
        </label>

        {error && (
          <p className="mb-3 text-sm text-red-600 dark:text-red-400" role="alert">
            {error}
          </p>
        )}

        <div className="flex justify-end gap-2">
          <button
            onClick={onClose}
            className="rounded-lg px-3 py-1.5 text-sm text-slate-500 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-700"
          >
            Cancel
          </button>
          <button
            onClick={create}
            disabled={busy}
            className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white transition-colors hover:bg-accent-hover disabled:opacity-50"
          >
            {busy ? "Creating…" : "Create"}
          </button>
        </div>
      </div>
    </div>
  );
}
