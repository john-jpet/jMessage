import { useState, type KeyboardEvent } from "react";

interface Props {
  onSend: (body: string) => void;
  onTyping: () => void;
}

export default function Composer({ onSend, onTyping }: Props) {
  const [text, setText] = useState("");

  function send() {
    const body = text.trim();
    if (!body) return;
    onSend(body);
    setText("");
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  }

  return (
    <div className="flex gap-2 border-t border-slate-200 bg-white p-3">
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
  );
}
