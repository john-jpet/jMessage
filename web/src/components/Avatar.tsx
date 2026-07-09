interface Props {
  id: string; // stable identity (userID or convID) drives the color
  name: string;
  size?: "sm" | "md" | "lg";
  online?: boolean; // undefined = no presence dot at all
}

/** Avatar renders deterministic initials on a hue derived from the ID —
 *  no uploads, no network, always consistent across sessions/devices. */
export default function Avatar({ id, name, size = "md", online }: Props) {
  const hue = hashHue(id);
  const initials = name
    .split(/\s+/)
    .map((w) => w[0])
    .slice(0, 2)
    .join("")
    .toUpperCase();
  const dims = { sm: "h-7 w-7 text-[10px]", md: "h-9 w-9 text-xs", lg: "h-16 w-16 text-xl" }[size];
  const dot = { sm: "h-2 w-2", md: "h-2.5 w-2.5", lg: "h-4 w-4" }[size];

  return (
    <span className="relative inline-block shrink-0" aria-hidden="true">
      <span
        className={`flex ${dims} items-center justify-center rounded-full font-semibold text-white`}
        style={{ backgroundColor: `hsl(${hue} 55% 48%)` }}
      >
        {initials || "?"}
      </span>
      {online !== undefined && (
        <span
          className={`absolute -bottom-0.5 -right-0.5 ${dot} rounded-full border-2 border-white dark:border-slate-900 ${
            online ? "bg-green-500" : "bg-slate-400"
          }`}
        />
      )}
    </span>
  );
}

function hashHue(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  return h % 360;
}
