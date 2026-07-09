import { ACCENTS, useSettings, type AccentName } from "../state/settings";
import { ensureNotifyPermission } from "../lib/notify";
import { useEscape } from "../lib/dialog";

export default function SettingsDialog({ onClose }: { onClose: () => void }) {
  const { settings, update } = useSettings();
  useEscape(onClose);

  return (
    <div
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
      role="dialog"
      aria-label="Settings"
    >
      <div
        className="max-h-full w-[26rem] overflow-y-auto rounded-xl bg-white p-5 shadow-xl animate-fade-in dark:bg-slate-800 dark:text-slate-100"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-lg font-semibold">Settings</h3>
          <button
            onClick={onClose}
            aria-label="Close settings"
            className="rounded px-2 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200"
          >
            ✕
          </button>
        </div>

        <Section title="Appearance">
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
                    settings.accent === name ? "scale-110 ring-2 ring-offset-2 ring-slate-400 dark:ring-offset-slate-800" : ""
                  }`}
                  style={{ backgroundColor: ACCENTS[name][0] }}
                />
              ))}
            </div>
          </Row>
        </Section>

        <Section title="Accessibility">
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
        </Section>

        <Section title="Notifications">
          <Toggle
            label="Sound effects"
            checked={settings.sound}
            onChange={(v) => update({ sound: v })}
          />
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
        </Section>
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mb-5">
      <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-400">{title}</h4>
      <div className="space-y-3">{children}</div>
    </section>
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
          className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform ${
            checked ? "translate-x-4" : "translate-x-0.5"
          }`}
        />
      </button>
    </label>
  );
}
