// Shared sort dropdown. The values are the canonical `order` keys understood by
// the backend (see internal/db/videos.go orderClause): empty / date_desc is the
// default newest-first ordering.

export const SORT_OPTIONS = [
  { value: "date_desc", label: "日期 ↓ (最新优先)" },
  { value: "date_asc", label: "日期 ↑ (最早优先)" },
  { value: "name_asc", label: "文件名 A→Z" },
  { value: "name_desc", label: "文件名 Z→A" },
] as const;

// Favorites can additionally sort by when each item was favorited — and do by
// default. The backend only honours these on a favorites listing.
export const FAV_SORT_OPTIONS = [
  { value: "fav_desc", label: "收藏时间 ↓ (最近收藏优先)" },
  { value: "fav_asc", label: "收藏时间 ↑ (最早收藏优先)" },
  ...SORT_OPTIONS,
] as const;

// A collection sorts the same way, by when each item was added to it — the
// backend treats fav_* on a collection listing as "time added".
export const COLLECTION_SORT_OPTIONS = [
  { value: "fav_desc", label: "加入时间 ↓ (最近加入优先)" },
  { value: "fav_asc", label: "加入时间 ↑ (最早加入优先)" },
  ...SORT_OPTIONS,
] as const;

export type SortValue = (typeof FAV_SORT_OPTIONS)[number]["value"];

export const DEFAULT_SORT: SortValue = "date_desc";
export const FAV_DEFAULT_SORT: SortValue = "fav_desc";

type SortOption = { value: SortValue; label: string };

// normalizeSort coerces an arbitrary string (e.g. from the URL) to a sort value
// valid for the given option set, falling back to that page's default. An
// absent param maps to the default so it behaves like the backend default.
export function normalizeSort(
  s: string | null | undefined,
  options: readonly SortOption[] = SORT_OPTIONS,
  fallback: SortValue = DEFAULT_SORT,
): SortValue {
  if (!s) return fallback;
  return options.some((o) => o.value === s) ? (s as SortValue) : fallback;
}

export function SortSelect({
  value,
  onChange,
  className,
  options = SORT_OPTIONS,
}: {
  value: SortValue;
  onChange: (v: SortValue) => void;
  className?: string;
  options?: readonly SortOption[];
}) {
  return (
    <select
      className={className ?? "field field-select field-sm w-auto"}
      value={value}
      onChange={(e) => onChange(e.target.value as SortValue)}
      title="排序方式"
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}
