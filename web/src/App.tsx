import { useState } from "react";
import { getToken, getUser } from "./api/client";
import type { UserSummary } from "./api/types";
import Login from "./pages/Login";
import Chat from "./pages/Chat";

export default function App() {
  const [user, setUser] = useState<UserSummary | null>(() =>
    getToken() ? getUser() : null,
  );
  if (!user) return <Login onSession={setUser} />;
  return <Chat self={user} onLogout={() => setUser(null)} />;
}
