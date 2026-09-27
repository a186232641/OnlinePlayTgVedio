import { MediaItem } from "./api/client";
import { SortValue } from "./components/SortSelect";

// dayLabel turns a timestamp into a section heading in the viewer's local time:
// 今天 / 昨天 / 2026-09-14 (周一).
const WEEKDAYS = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];
export function dayLabel(iso?: string): string | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return null;
  const startOf = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((startOf(new Date()) - startOf(d)) / 86_400_000);
  if (days === 0) return "今天";
  if (days === 1) return "昨天";
  const ymd = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  return `${ymd} (${WEEKDAYS[d.getDay()]})`;
}

// groupFor picks the day to group by for the active sort: favorite (or
// added-to-collection) time when sorted by it, publish date when sorted by date, nothing for name sorts (a
// name order scatters any one day across the whole list).
export function groupFor(order: SortValue): ((m: MediaItem) => string | null) | undefined {
  if (order.startsWith("fav")) return (m) => dayLabel(m.favorited_at);
  if (order.startsWith("date")) return (m) => dayLabel(m.date) ?? "未知日期";
  return undefined;
}
