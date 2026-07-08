import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Conversation, UserSummary } from "../api/types";

interface Props {
  onClose: () => void;
  onCreated: (convID: string) => void;
}

export default function NewConversationDialog({ onClose, onCreated }: Props) {
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
      if (names.length === 0) throw new Error("enter at least one username");
      if (mode === "dm" && names.length !== 1) throw new Error("a DM takes exactly one username");

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
      setError(err instanceof Error ? err.message : "failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 flex items-center justify-center bg-black/30" onClick={onClose}>
      <div
        className="w-96 rounded-lg bg-white p-5 shadow-lg"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="mb-3 font-semibold text-slate-800">New conversation</h3>

        <div className="mb-3 flex gap-2 text-sm">
          {(["dm", "group"] as const).map((m) => (
            <button
              key={m}
              onClick={() => setMode(m)}
              className={`rounded px-3 py-1 ${
                mode === m ? "bg-blue-600 text-white" : "bg-slate-100 text-slate-600"
              }`}
            >
              {m === "dm" ? "Direct message" : "Group"}
            </button>
          ))}
        </div>

        {mode === "group" && (
          <label className="mb-2 block text-sm">
            <span className="text-slate-600">Group name</span>
            <input
              className="mt-1 w-full rounded border border-slate-300 px-2 py-1.5"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
        )}
        <label className="mb-3 block text-sm">
          <span className="text-slate-600">
            {mode === "dm" ? "Username" : "Usernames (comma-separated)"}
          </span>
          <input
            className="mt-1 w-full rounded border border-slate-300 px-2 py-1.5"
            value={usernames}
            onChange={(e) => setUsernames(e.target.value)}
            autoFocus
          />
        </label>

        {error && <p className="mb-3 text-sm text-red-600">{error}</p>}

        <div className="flex justify-end gap-2">
          <button onClick={onClose} className="rounded px-3 py-1.5 text-sm text-slate-500">
            Cancel
          </button>
          <button
            onClick={create}
            disabled={busy}
            className="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
          >
            Create
          </button>
        </div>
      </div>
    </div>
  );
}
