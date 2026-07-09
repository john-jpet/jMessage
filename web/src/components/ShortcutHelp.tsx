import { useEscape } from "../lib/dialog";

const SHORTCUTS: [string, string][] = [
  ["Enter", "Send message"],
  ["Shift + Enter", "New line"],
  ["Ctrl + K", "Quick conversation switcher"],
  ["Ctrl + /", "This shortcut reference"],
  ["Escape", "Close dialogs"],
  ["Tab", "Navigate the interface"],
];

export default function ShortcutHelp({ onClose }: { onClose: () => void }) {
  useEscape(onClose);
  return (
    <div
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
      role="dialog"
      aria-label="Keyboard shortcuts"
    >
      <div
        className="w-80 rounded-xl bg-white p-5 shadow-xl animate-fade-in dark:bg-slate-800 dark:text-slate-100"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="mb-3 text-lg font-semibold">Keyboard shortcuts</h3>
        <dl className="space-y-2">
          {SHORTCUTS.map(([keys, action]) => (
            <div key={keys} className="flex items-center justify-between gap-4">
              <dt>
                <kbd className="rounded border border-slate-300 bg-slate-50 px-1.5 py-0.5 font-mono text-[11px] text-slate-700 dark:border-slate-600 dark:bg-slate-700 dark:text-slate-200">
                  {keys}
                </kbd>
              </dt>
              <dd className="text-sm text-slate-600 dark:text-slate-300">{action}</dd>
            </div>
          ))}
        </dl>
      </div>
    </div>
  );
}
