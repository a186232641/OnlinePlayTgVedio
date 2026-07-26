// Shared sort dropdown. The values are the canonical `order` keys understood by
// the backend (see internal/db/videos.go orderClause): empty / date_desc is the
// default newest-first ordering.

export const SORT_OPTIONS = [
  { value: "date_desc", label: "日期 ↓ (最新优先)" },
  { value: "date_asc", label: "日期 ↑ (最早优先)" },
  { value: "name_asc", label: "文件名 A→Z" },
  { value: "name_desc", label: "文件名 Z→A" },
] as const;

export type SortValue = (typeof SORT_OPTIONS)[number]["value"];

export const DEFAULT_SORT: SortValue = "date_desc";

// normalizeSort coerces an arbitrary string (e.g. from the URL) to a known sort
// value, falling back to the default. Keeps "" → date_desc so an absent param
// behaves like the backend default.
export function normalizeSort(s: string | null | undefined): SortValue {
  if (!s) return DEFAULT_SORT;
  return SORT_OPTIONS.some((o) => o.value === s) ? (s as SortValue) : DEFAULT_SORT;
}

export function SortSelect({
  value,
  onChange,
  className,
}: {
  value: SortValue;
  onChange: (v: SortValue) => void;
  className?: string;
}) {
  return (
    <select
      className={className ?? "px-3 py-2 bg-slate-800 rounded text-sm"}
      value={value}
      onChange={(e) => onChange(e.target.value as SortValue)}
      title="排序方式"
    >
      {SORT_OPTIONS.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}
