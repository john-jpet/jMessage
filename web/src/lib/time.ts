/** relativeTime renders a compact "how long ago" label for lists. */
export function relativeTime(ts: number): string {
  if (!ts) return "";
  const diff = Date.now() - ts;
  if (diff < 60_000) return "now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m`;
  if (diff < 86_400_000 && isSameDay(ts, Date.now())) {
    return `${Math.floor(diff / 3_600_000)}h`;
  }
  if (isSameDay(ts, Date.now() - 86_400_000)) return "yesterday";
  return new Date(ts).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

/** lastSeenLabel is the sentence form used in headers/profiles. */
export function lastSeenLabel(ts: number): string {
  if (!ts) return "offline";
  const r = relativeTime(ts);
  return r === "now" ? "last seen just now" : `last seen ${r === "yesterday" ? r : r + " ago"}`;
}

export function isSameDay(a: number, b: number): boolean {
  const da = new Date(a);
  const db = new Date(b);
  return (
    da.getFullYear() === db.getFullYear() &&
    da.getMonth() === db.getMonth() &&
    da.getDate() === db.getDate()
  );
}

/** dayLabel names a date separator: Today, Yesterday, weekday within a
 *  week, otherwise a full date. */
export function dayLabel(ts: number): string {
  const now = Date.now();
  if (isSameDay(ts, now)) return "Today";
  if (isSameDay(ts, now - 86_400_000)) return "Yesterday";
  if (now - ts < 6 * 86_400_000) {
    return new Date(ts).toLocaleDateString(undefined, { weekday: "long" });
  }
  return new Date(ts).toLocaleDateString(undefined, {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
}

export function clockTime(ts: number): string {
  return new Date(ts).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
}
