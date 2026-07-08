import { useState, type FormEvent } from "react";
import { api, setSession } from "../api/client";
import type { Session, UserSummary } from "../api/types";

export default function Login({ onSession }: { onSession: (u: UserSummary) => void }) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      const body =
        mode === "register"
          ? { username, password, displayName: displayName || username }
          : { username, password };
      const sess = await api<Session>("POST", `/api/${mode}`, body);
      setSession(sess.token, sess.user);
      onSession(sess.user);
    } catch (err) {
      setError(err instanceof Error ? err.message : "request failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-100">
      <form onSubmit={submit} className="w-80 rounded-lg bg-white p-6 shadow">
        <h1 className="mb-1 text-xl font-bold text-slate-800">jMessage</h1>
        <p className="mb-4 text-sm text-slate-500">
          {mode === "login" ? "Sign in to continue" : "Create your account"}
        </p>

        <label className="mb-2 block text-sm">
          <span className="text-slate-600">Username</span>
          <input
            className="mt-1 w-full rounded border border-slate-300 px-2 py-1.5"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoFocus
            required
          />
        </label>
        {mode === "register" && (
          <label className="mb-2 block text-sm">
            <span className="text-slate-600">Display name (optional)</span>
            <input
              className="mt-1 w-full rounded border border-slate-300 px-2 py-1.5"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          </label>
        )}
        <label className="mb-4 block text-sm">
          <span className="text-slate-600">Password</span>
          <input
            type="password"
            className="mt-1 w-full rounded border border-slate-300 px-2 py-1.5"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            minLength={8}
          />
        </label>

        {error && <p className="mb-3 text-sm text-red-600">{error}</p>}

        <button
          type="submit"
          disabled={busy}
          className="w-full rounded bg-blue-600 py-2 text-white hover:bg-blue-700 disabled:opacity-50"
        >
          {mode === "login" ? "Sign in" : "Register"}
        </button>
        <button
          type="button"
          onClick={() => setMode(mode === "login" ? "register" : "login")}
          className="mt-3 w-full text-sm text-blue-600 hover:underline"
        >
          {mode === "login" ? "New here? Create an account" : "Have an account? Sign in"}
        </button>
      </form>
    </div>
  );
}
