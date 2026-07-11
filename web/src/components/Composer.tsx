import { useEffect, useRef, useState, type ClipboardEvent, type KeyboardEvent } from "react";

interface Props {
  files: File[];
  onFilesChange: (files: File[]) => void;
  onSend: (body: string, files: File[]) => void;
  onTyping: () => void;
  /** Changing this refocuses the textarea (conversation switches). */
  focusKey: string;
}

const MAX_FILES = 10;
const MAX_BODY_CHARS = 2000; // mirrors internal/validation on the server
const COUNTER_THRESHOLD = 1800;
const MAX_FILE_BYTES = 10 << 20; // 10 MB — rejected before any upload

export default function Composer({ files, onFilesChange, onSend, onTyping, focusKey }: Props) {
  const [text, setText] = useState("");
  const [fileError, setFileError] = useState("");
  const taRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const charCount = [...text].length; // code points, matching the server
  const tooLong = charCount > MAX_BODY_CHARS;
  const canSend = (text.trim().length > 0 || files.length > 0) && !tooLong;

  // Refocus on conversation switch — typing should never need a click.
  useEffect(() => {
    taRef.current?.focus();
  }, [focusKey]);

  // Auto-grow: 1 line minimum, ~8 lines maximum, scroll after.
  function autoGrow() {
    const ta = taRef.current;
    if (!ta) return;
    ta.style.height = "auto";
    ta.style.height = `${Math.min(ta.scrollHeight, 200)}px`;
  }

  function send() {
    if (!canSend) return;
    onSend(text.trim(), files);
    setText("");
    onFilesChange([]);
    if (fileInputRef.current) fileInputRef.current.value = "";
    requestAnimationFrame(autoGrow);
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  }

  function addFiles(list: FileList | File[] | null) {
    if (!list) return;
    const incoming = Array.from(list);
    const oversized = incoming.filter((f) => f.size > MAX_FILE_BYTES);
    const ok = incoming.filter((f) => f.size <= MAX_FILE_BYTES);
    setFileError(
      oversized.length > 0
        ? `${oversized.map((f) => f.name).join(", ")} exceed${oversized.length === 1 ? "s" : ""} the 10 MB limit and won't be attached.`
        : "",
    );
    if (ok.length > 0) onFilesChange([...files, ...ok].slice(0, MAX_FILES));
  }

  // Paste image support: screenshots land straight in the composer.
  function onPaste(e: ClipboardEvent<HTMLTextAreaElement>) {
    const pasted = Array.from(e.clipboardData.files);
    if (pasted.length > 0) {
      e.preventDefault();
      addFiles(pasted);
    }
  }

  return (
    <div className="border-t border-slate-200 bg-white p-3 dark:border-slate-700 dark:bg-slate-900">
      {fileError && (
        <p className="mb-2 text-xs text-red-600 dark:text-red-400" role="alert">
          {fileError}
        </p>
      )}
      {files.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-2">
          {files.map((f, i) => (
            <span
              key={`${f.name}-${i}`}
              className="flex items-center gap-1 rounded-lg bg-slate-100 px-2 py-1 text-xs text-slate-700 animate-fade-in dark:bg-slate-800 dark:text-slate-200"
            >
              📎 {f.name} <span className="text-slate-400">({formatSize(f.size)})</span>
              <button
                onClick={() => onFilesChange(files.filter((_, j) => j !== i))}
                className="ml-1 text-slate-400 hover:text-red-600"
                aria-label={`Remove ${f.name}`}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
      <div className="flex items-end gap-2">
        <input
          ref={fileInputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => addFiles(e.target.files)}
          aria-hidden="true"
          tabIndex={-1}
        />
        <button
          onClick={() => fileInputRef.current?.click()}
          className="rounded-lg border border-slate-300 px-3 py-2 text-slate-500 transition-colors hover:bg-slate-50 dark:border-slate-600 dark:text-slate-400 dark:hover:bg-slate-800"
          title="Attach files"
          aria-label="Attach files"
        >
          📎
        </button>
        <div className="relative flex-1">
          <textarea
            ref={taRef}
            rows={1}
            className="w-full resize-none rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 placeholder:text-slate-400 focus:border-accent focus:outline-none dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100"
            placeholder="Type a message…"
            aria-label="Message"
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              autoGrow();
              if (e.target.value) onTyping();
            }}
            onKeyDown={onKeyDown}
            onPaste={onPaste}
          />
          {charCount > COUNTER_THRESHOLD && (
            <span
              className={`absolute -top-5 right-1 text-[10px] ${
                tooLong ? "font-semibold text-red-600" : "text-slate-400"
              }`}
              role="status"
            >
              {charCount} / {MAX_BODY_CHARS}
            </span>
          )}
        </div>
        <button
          onClick={send}
          disabled={!canSend}
          aria-label="Send message"
          className="rounded-lg bg-accent px-4 py-2 text-sm text-white transition-colors hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-40"
        >
          Send
        </button>
      </div>
    </div>
  );
}

export function formatSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1 << 20)).toFixed(1)} MB`;
}
