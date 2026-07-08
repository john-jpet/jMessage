import { useRef, useState, type KeyboardEvent } from "react";

interface Props {
  onSend: (body: string, files: File[]) => void;
  onTyping: () => void;
}

const MAX_FILES = 10;

export default function Composer({ onSend, onTyping }: Props) {
  const [text, setText] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const fileInputRef = useRef<HTMLInputElement>(null);

  function send() {
    const body = text.trim();
    if (!body && files.length === 0) return;
    onSend(body, files);
    setText("");
    setFiles([]);
    if (fileInputRef.current) fileInputRef.current.value = "";
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  }

  function addFiles(list: FileList | null) {
    if (!list) return;
    setFiles((prev) => [...prev, ...Array.from(list)].slice(0, MAX_FILES));
  }

  return (
    <div className="border-t border-slate-200 bg-white p-3">
      {files.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-2">
          {files.map((f, i) => (
            <span
              key={`${f.name}-${i}`}
              className="flex items-center gap-1 rounded bg-slate-100 px-2 py-1 text-xs text-slate-700"
            >
              📎 {f.name} <span className="text-slate-400">({formatSize(f.size)})</span>
              <button
                onClick={() => setFiles((prev) => prev.filter((_, j) => j !== i))}
                className="ml-1 text-slate-400 hover:text-red-600"
                aria-label={`remove ${f.name}`}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
      <div className="flex gap-2">
        <input
          ref={fileInputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => addFiles(e.target.files)}
        />
        <button
          onClick={() => fileInputRef.current?.click()}
          className="rounded border border-slate-300 px-3 text-slate-500 hover:bg-slate-50"
          title="Attach files"
          aria-label="Attach files"
        >
          📎
        </button>
        <textarea
          rows={1}
          className="flex-1 resize-none rounded border border-slate-300 px-3 py-2 text-sm focus:outline-blue-400"
          placeholder="Type a message… (Enter to send)"
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            if (e.target.value) onTyping();
          }}
          onKeyDown={onKeyDown}
        />
        <button
          onClick={send}
          className="rounded bg-blue-600 px-4 text-sm text-white hover:bg-blue-700"
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
