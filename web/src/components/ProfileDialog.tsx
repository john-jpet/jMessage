import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Conversation, UserSummary } from "../api/types";
import { useEscape } from "../lib/dialog";
import { lastSeenLabel } from "../lib/time";
import Avatar from "./Avatar";

interface ProfileData {
  user: UserSummary;
  createdAt: number;
  lastSeen: number;
  sharedConversations: Conversation[];
}

interface Props {
  userID: string;
  livePresence?: boolean; // live override from the presence map
  onOpenConversation: (id: string) => void;
  onClose: () => void;
}

export default function ProfileDialog({ userID, livePresence, onOpenConversation, onClose }: Props) {
  useEscape(onClose);
  const { data, isLoading } = useQuery({
    queryKey: ["profile", userID],
    queryFn: () => api<ProfileData>("GET", `/api/users/${userID}/profile`),
  });

  const online = livePresence ?? data?.user.online ?? false;

  return (
    <div
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
      role="dialog"
      aria-label="User profile"
    >
      <div
        className="w-80 rounded-xl bg-white p-5 shadow-xl animate-fade-in dark:bg-slate-800 dark:text-slate-100"
        onClick={(e) => e.stopPropagation()}
      >
        {isLoading || !data ? (
          <div className="flex flex-col items-center gap-3 py-6" aria-busy="true">
            <span className="h-16 w-16 rounded-full bg-slate-200 animate-pulse-soft dark:bg-slate-700" />
            <span className="h-4 w-32 rounded bg-slate-200 animate-pulse-soft dark:bg-slate-700" />
          </div>
        ) : (
          <>
            <div className="flex flex-col items-center gap-2 text-center">
              <Avatar
                id={data.user.id}
                name={data.user.displayName}
                size="lg"
                online={online}
                avatarID={data.user.avatarID}
              />
              <div>
                <p className="font-semibold">{data.user.displayName}</p>
                <p className="text-xs text-slate-400">@{data.user.username}</p>
                {data.user.status && (
                  <p className="mt-1 max-w-56 text-xs italic text-slate-500 dark:text-slate-400">
                    “{data.user.status}”
                  </p>
                )}
              </div>
              <p className="text-xs text-slate-500 dark:text-slate-400">
                {online ? (
                  <span className="text-green-600 dark:text-green-400">● online</span>
                ) : (
                  lastSeenLabel(data.lastSeen)
                )}
              </p>
            </div>

            <dl className="mt-4 space-y-1.5 border-t border-slate-200 pt-3 text-sm dark:border-slate-700">
              <div className="flex justify-between">
                <dt className="text-slate-400">Joined</dt>
                <dd>
                  {new Date(data.createdAt).toLocaleDateString(undefined, {
                    year: "numeric",
                    month: "long",
                    day: "numeric",
                  })}
                </dd>
              </div>
            </dl>

            {data.sharedConversations.length > 0 && (
              <div className="mt-3 border-t border-slate-200 pt-3 dark:border-slate-700">
                <p className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-slate-400">
                  Shared conversations
                </p>
                <ul className="max-h-32 space-y-0.5 overflow-y-auto">
                  {data.sharedConversations.map((c) => (
                    <li key={c.id}>
                      <button
                        onClick={() => {
                          onOpenConversation(c.id);
                          onClose();
                        }}
                        className="w-full truncate rounded px-2 py-1 text-left text-sm text-slate-700 hover:bg-slate-50 dark:text-slate-300 dark:hover:bg-slate-700"
                      >
                        {c.displayName || c.name || "Conversation"}
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
