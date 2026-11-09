const pad = (n: number) => String(n).padStart(2, "0");

/**
 * Compact timestamp for tables: "2026-11-08 19:23".
 * Accepts the backend's long form ("Sun 2026-11-08 7:23 PM CST (UTC+08:00)"),
 * "2026-11-08 19:23:45", and ISO strings. Unknown formats are returned as-is.
 */
export function formatTime(value?: string | null) {
  if (!value) return "—";
  const raw = String(value).trim();
  const long = raw.match(/(\d{4}-\d{2}-\d{2})\s+(\d{1,2}):(\d{2})(?::\d{2})?\s*(AM|PM)?/i);
  if (long && !raw.includes("T")) {
    let hour = Number(long[2]);
    const ampm = long[4]?.toUpperCase();
    if (ampm === "PM" && hour < 12) hour += 12;
    if (ampm === "AM" && hour === 12) hour = 0;
    return `${long[1]} ${pad(hour)}:${long[3]}`;
  }
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return raw;
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
export function limitText(used: number, limit: number) {
  return limit > 0 ? `${used} / ${limit}` : `${used} / ∞`;
}
