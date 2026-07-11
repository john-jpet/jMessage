import { useState } from "react";
import { getToken, getUser, updateSessionUser } from "./api/client";
import type { UserSummary } from "./api/types";
import { useRoute } from "./lib/route";
import Login from "./pages/Login";
import Chat from "./pages/Chat";
import Settings from "./pages/Settings";

export default function App() {
  const [user, setUser] = useState<UserSummary | null>(() => (getToken() ? getUser() : null));
  const [route, navigate] = useRoute();

  if (!user) return <Login onSession={setUser} />;

  return (
    <>
      <Chat
        self={user}
        onLogout={() => {
          setUser(null);
          navigate("/");
        }}
        onOpenSettings={() => navigate("/settings")}
      />
      {route === "/settings" && (
        <Settings
          self={user}
          onBack={() => navigate("/")}
          onProfileChange={(u) => {
            updateSessionUser(u);
            setUser(u);
          }}
        />
      )}
    </>
  );
}
