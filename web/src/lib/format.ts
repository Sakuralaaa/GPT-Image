export function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value.includes("T") ? value : value.replace(" ", "T"));
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-CN", { hour12: false });
}

export function limitText(used: number, limit: number) {
  return limit > 0 ? `${used} / ${limit}` : `${used} / ∞`;
}
