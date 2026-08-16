import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, uploadBlob } from "../api/client";
import type { SettingsProfile, UserSummary } from "../api/types";
import { resizeImage } from "../lib/image";
import { ensureNotifyPermission } from "../lib/notify";
import { ACCENTS, useSettings, type AccentName } from "../state/settings";
import Avatar from "../components/Avatar";

const STATUS_MAX = 140;
const DISPLAY_NAME_MAX = 64;
const AVATAR_MAX_BYTES = 5 << 20;

interface Props {
  self: UserSummary;
  onBack: () => void;
  /** Propagates saved profile changes into the app shell + session. */
  onProfileChange: (u: UserSummary) => void;
}

export default function Settings({ self, onBack, onProfileChange }: Props) {
  const { settings, update } = useSettings();
  const queryClient = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [uploadingAvatar, setUploadingAvatar] = useState(false);
  const [draft, setDraft] = useState<{
    displayName: string;
    status: string;
    avatarAttachmentID: string;
  } | null>(null);

  const { data: profile } = useQuery({
    queryKey: ["settingsProfile"],
    queryFn: () => api<SettingsProfile>("GET", "/api/settings/profile"),
  });

  const displayName = draft?.displayName ?? profile?.displayName ?? "";
  const status = draft?.status ?? profile?.status ?? "";
  const avatarAttachmentID = draft?.avatarAttachmentID ?? profile?.avatarID ?? "";
  const dirty =
    draft !== null &&
    (draft.displayName !== (profile?.displayName ?? "") ||
      draft.status !== (profile?.status ?? "") ||
      draft.avatarAttachmentID !== (profile?.avatarID ?? ""));

  function updateDraft(changes: Partial<NonNullable<typeof draft>>) {
    setDraft((current) => ({
      displayName: current?.displayName ?? profile?.displayName ?? "",
      status: current?.status ?? profile?.status ?? "",
      avatarAttachmentID: current?.avatarAttachmentID ?? profile?.avatarID ?? "",
      ...changes,
    }));
  }

  const patch = useMutation({
    mutationFn: (body: Record<string, string>) =>
      api<SettingsProfile>("PATCH", "/api/settings/profile", body),
    onSuccess: (p) => {
      queryClient.setQueryData(["settingsProfile"], p);
      queryClient.invalidateQueries({ queryKey: ["conversations"] });
      queryClient.invalidateQueries({ queryKey: ["profile"] });
      onProfileChange({
        ...self,
        displayName: p.displayName || p.username,
        status: p.status,
        avatarID: p.avatarID,
      });
      setDraft(null);
      setError("");
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    },
    onError: (err) => setError(err instanceof Error ? err.message : "save failed"),
  });

  async function pickAvatar(file: File | undefined) {
    if (!file) return;
    setError("");
    setUploadingAvatar(true);
    try {
      const resized = await resizeImage(file, 256);
      if (resized.size > AVATAR_MAX_BYTES) {
        setError("Avatar is too large (max 5 MB).");
        return;
      }
      const named = new File([resized], "avatar.jpg", {
        type: resized.type || "image/jpeg",
      });
      const ref = await uploadBlob(named, "avatar.jpg");
      updateDraft({ avatarAttachmentID: ref.id });
    } catch (err) {
      setError(err instanceof Error ? err.message : "avatar upload failed");
    } finally {
      setUploadingAvatar(false);
      if (fileRef.current) fileRef.current.value = "";
    }
  }

  const inputCls =
    "mt-1 w-full rounded-lg border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-900 focus:border-accent focus:outline-none dark:border-slate-600 dark:bg-slate-700 dark:text-slate-100";

  return (
    // Overlay (not a route swap of <Chat>): the messaging WebSocket
    // underneath stays connected while the user edits settings.
    <div className="fixed inset-0 z-30 overflow-y-auto bg-slate-100 dark:bg-slate-950">
      <div className="mx-auto max-w-2xl px-4 py-6">
        <header className="mb-6 flex items-center gap-3">
          <button
            onClick={onBack}
            aria-label="Back to chats"
            className="rounded-lg px-2 py-1 text-slate-500 hover:bg-slate-200 dark:text-slate-400 dark:hover:bg-slate-800"
          >
            ← Back
          </button>
          <h1 className="text-xl font-bold text-slate-800 dark:text-slate-100">Settings</h1>
          {saved && (
            <span className="ml-auto text-sm text-green-600 animate-fade-in dark:text-green-400" role="status">
              Saved ✓
            </span>
          )}
        </header>

        <Card title="Profile">
          {!profile ? (
            <div className="h-24 animate-pulse-soft rounded bg-slate-200 dark:bg-slate-700" aria-busy="true" />
          ) : (
            <>
              <div className="mb-4 flex items-center gap-4">
                <Avatar
                  id={profile.id}
                  name={displayName || profile.username}
                  size="lg"
                  avatarID={avatarAttachmentID || undefined}
                />
                <div className="flex flex-col gap-1.5">
                  <input
                    ref={fileRef}
                    type="file"
                    accept="image/png,image/jpeg,image/gif"
                    className="hidden"
                    onChange={(e) => void pickAvatar(e.target.files?.[0])}
                  />
                  <button
                    onClick={() => fileRef.current?.click()}
                    disabled={uploadingAvatar || patch.isPending}
                    className="rounded-lg bg-accent px-3 py-1 text-xs text-white hover:bg-accent-hover"
                  >
                    {uploadingAvatar
                      ? "Uploading…"
                      : avatarAttachmentID
                        ? "Change picture"
                        : "Upload picture"}
                  </button>
                  {avatarAttachmentID && (
                    <button
                      onClick={() => updateDraft({ avatarAttachmentID: "" })}
                      disabled={uploadingAvatar || patch.isPending}
                      className="rounded-lg px-3 py-1 text-xs text-slate-500 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-700"
                    >
                      Remove picture
                    </button>
                  )}
                </div>
              </div>

              <label className="mb-3 block text-sm">
                <span className="text-slate-600 dark:text-slate-300">
                  Display name{" "}
                  <span className="text-xs text-slate-400">(optional — username shown if empty)</span>
                </span>
                <input
                  className={inputCls}
                  value={displayName}
                  maxLength={DISPLAY_NAME_MAX}
                  onChange={(e) => updateDraft({ displayName: e.target.value })}
                />
              </label>

              <label className="mb-3 block text-sm">
                <span className="flex items-baseline justify-between text-slate-600 dark:text-slate-300">
                  Status
                  <span
                    className={`text-xs ${status.length > STATUS_MAX ? "text-red-600" : "text-slate-400"}`}
                  >
                    {status.length}/{STATUS_MAX}
                  </span>
                </span>
                <input
                  className={inputCls}
                  placeholder="What's happening?"
                  value={status}
                  onChange={(e) => updateDraft({ status: e.target.value })}
                />
              </label>

              <dl className="mb-4 space-y-1 text-sm">
                <InfoRow label="Username">@{profile.username}</InfoRow>
                <InfoRow label="Joined">
                  {new Date(profile.createdAt).toLocaleDateString(undefined, {
                    year: "numeric",
                    month: "long",
                    day: "numeric",
                  })}
                </InfoRow>
                <InfoRow label="Account ID">
                  <button
                    onClick={() => navigator.clipboard?.writeText(profile.id)}
                    title="Copy account ID"
                    className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-xs text-slate-600 hover:bg-slate-200 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600"
                  >
                    {profile.id} 📋
                  </button>
                </InfoRow>
              </dl>

              {error && (
                <p className="mb-3 text-sm text-red-600 dark:text-red-400" role="alert">
                  {error}
                </p>
              )}
              <button
                onClick={() => patch.mutate({ displayName, status, avatarAttachmentID })}
                disabled={!dirty || patch.isPending || uploadingAvatar || status.length > STATUS_MAX}
                className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white hover:bg-accent-hover disabled:opacity-40"
              >
                {patch.isPending ? "Saving…" : "Save changes"}
              </button>
            </>
          )}
        </Card>

        <Card title="Appearance">
          <Row label="Theme">
            <div className="flex gap-1">
              {(["light", "dark", "system"] as const).map((t) => (
                <Chip key={t} active={settings.theme === t} onClick={() => update({ theme: t })}>
                  {t}
                </Chip>
              ))}
            </div>
          </Row>
          <Row label="Accent">
            <div className="flex gap-2">
              {(Object.keys(ACCENTS) as AccentName[]).map((name) => (
                <button
                  key={name}
                  onClick={() => update({ accent: name })}
                  aria-label={`accent ${name}`}
                  className={`h-6 w-6 rounded-full transition-transform ${
                    settings.accent === name
                      ? "scale-110 ring-2 ring-slate-400 ring-offset-2 dark:ring-offset-slate-800"
                      : ""
                  }`}
                  style={{ backgroundColor: ACCENTS[name][0] }}
                />
              ))}
            </div>
          </Row>
        </Card>

        <Card title="Notifications">
          <Toggle label="Sound effects" checked={settings.sound} onChange={(v) => update({ sound: v })} />
          <Toggle
            label="Desktop notifications"
            checked={settings.desktopNotifications}
            onChange={(v) => {
              if (v) {
                void ensureNotifyPermission().then((ok) => update({ desktopNotifications: ok }));
              } else {
                update({ desktopNotifications: false });
              }
            }}
          />
          <Toggle
            label="Show message previews"
            checked={settings.showPreviews}
            onChange={(v) => update({ showPreviews: v })}
          />
        </Card>

        <Card title="Accessibility">
          <Row label="Font size">
            <div className="flex gap-1">
              {([0.875, 1, 1.125] as const).map((s, i) => (
                <Chip key={s} active={settings.fontScale === s} onClick={() => update({ fontScale: s })}>
                  {["Small", "Default", "Large"][i]}
                </Chip>
              ))}
            </div>
          </Row>
          <Toggle
            label="Reduced motion"
            checked={settings.reducedMotion}
            onChange={(v) => update({ reducedMotion: v })}
          />
          <Toggle
            label="High contrast"
            checked={settings.highContrast}
            onChange={(v) => update({ highContrast: v })}
          />
        </Card>
      </div>
    </div>
  );
}

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mb-4 rounded-xl bg-white p-5 shadow-sm dark:bg-slate-800 dark:text-slate-100">
      <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-slate-400">{title}</h2>
      <div className="space-y-3">{children}</div>
    </section>
  );
}

function InfoRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <dt className="text-slate-400">{label}</dt>
      <dd className="text-slate-700 dark:text-slate-200">{children}</dd>
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <span className="text-sm">{label}</span>
      {children}
    </div>
  );
}

function Chip({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={`rounded-full px-3 py-1 text-xs capitalize transition-colors ${
        active
          ? "bg-accent text-white"
          : "bg-slate-100 text-slate-600 hover:bg-slate-200 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600"
      }`}
    >
      {children}
    </button>
  );
}

function Toggle({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer items-center justify-between gap-4">
      <span className="text-sm">{label}</span>
      <button
        role="switch"
        aria-checked={checked}
        aria-label={label}
        onClick={() => onChange(!checked)}
        className={`relative h-5 w-9 rounded-full transition-colors ${
          checked ? "bg-accent" : "bg-slate-300 dark:bg-slate-600"
        }`}
      >
        <span
          className={`absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white transition-transform ${
            checked ? "translate-x-4" : "translate-x-0"
          }`}
        />
      </button>
    </label>
  );
}
