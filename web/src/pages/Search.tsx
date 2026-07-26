import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";

import { api, Channel, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";
import { SortSelect, SortValue, normalizeSort, DEFAULT_SORT } from "../components/SortSelect";

interface Page { videos: Video[] }

const PAGE_SIZE = 200;

interface Filters {
  text: string;
  fileName: string;
  dateFrom: string; // yyyy-mm-dd
  dateTo: string;
  channelID: number;
  order: SortValue;
}

// The URL query string is the single source of truth for the active search, so
// navigating into a video and pressing Back restores the exact same results.
// `draft` is the editable form state; submitting copies it into the URL.
function filtersFromParams(p: URLSearchParams): Filters {
  return {
    text: p.get("text") ?? "",
    fileName: p.get("file_name") ?? "",
    dateFrom: p.get("date_from") ?? "",
    dateTo: p.get("date_to") ?? "",
    channelID: Number(p.get("channel_id") ?? "0") || 0,
    order: normalizeSort(p.get("order")),
  };
}

function paramsFromFilters(f: Filters): URLSearchParams {
  const p = new URLSearchParams();
  if (f.text) p.set("text", f.text);
  if (f.fileName) p.set("file_name", f.fileName);
  if (f.dateFrom) p.set("date_from", f.dateFrom);
  if (f.dateTo) p.set("date_to", f.dateTo);
  if (f.channelID > 0) p.set("channel_id", String(f.channelID));
  if (f.order !== DEFAULT_SORT) p.set("order", f.order);
  return p;
}

function hasAny(f: Filters): boolean {
  return !!(f.text || f.fileName || f.dateFrom || f.dateTo);
}

export function Search() {
  const [searchParams, setSearchParams] = useSearchParams();
  const submitted = useMemo(() => filtersFromParams(searchParams), [searchParams]);

  const [draft, setDraft] = useState<Filters>(submitted);
  // Re-sync the form whenever the URL changes from outside the form (Back nav,
  // sort change) so the inputs reflect the active search.
  useEffect(() => { setDraft(submitted); }, [searchParams]); // eslint-disable-line react-hooks/exhaustive-deps

  const channels = useQuery<{ channels: Channel[] }>({
    queryKey: ["channels", "all"],
    queryFn: () => api.get("/api/channels/"),
  });

  const result = useInfiniteQuery<Page>({
    queryKey: ["search", submitted],
    enabled: hasAny(submitted),
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      const cursor = pageParam as number;
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE) });
      if (submitted.text) qs.set("text", submitted.text);
      if (submitted.fileName) qs.set("file_name", submitted.fileName);
      if (submitted.dateFrom) qs.set("date_from", submitted.dateFrom);
      if (submitted.dateTo) qs.set("date_to", submitted.dateTo);
      if (submitted.channelID > 0) qs.set("channel_id", String(submitted.channelID));
      if (submitted.order !== DEFAULT_SORT) qs.set("order", submitted.order);
      if (cursor > 0) qs.set("offset_id", String(cursor));
      return api.get<Page>(`/api/videos/search?${qs}`);
    },
    getNextPageParam: (last) => {
      if (last.videos.length < PAGE_SIZE) return undefined;
      return last.videos[last.videos.length - 1]?.id;
    },
  });

  const all = useMemo<Video[]>(
    () => result.data?.pages.flatMap((p) => p.videos) ?? [],
    [result.data],
  );

  // Sort applies immediately to the active search (re-sorts the URL), keeping
  // the rest of the submitted filters untouched.
  const changeOrder = (order: SortValue) =>
    setSearchParams(paramsFromFilters({ ...submitted, order }));

  return (
    <div>
      <form
        onSubmit={(e) => { e.preventDefault(); setSearchParams(paramsFromFilters(draft)); }}
        className="p-4 border-b border-slate-800 grid gap-2 sm:grid-cols-2 lg:grid-cols-5"
      >
        <input
          className="px-3 py-2 bg-slate-800 rounded text-sm sm:col-span-2"
          placeholder="文件名(file_name)…"
          value={draft.fileName}
          onChange={(e) => setDraft({ ...draft, fileName: e.target.value })}
        />
        <input
          className="px-3 py-2 bg-slate-800 rounded text-sm sm:col-span-2"
          placeholder="正文 / caption (text)…"
          value={draft.text}
          onChange={(e) => setDraft({ ...draft, text: e.target.value })}
        />
        <select
          className="px-3 py-2 bg-slate-800 rounded text-sm"
          value={draft.channelID}
          onChange={(e) => setDraft({ ...draft, channelID: Number(e.target.value) })}
        >
          <option value={0}>全部频道</option>
          {(channels.data?.channels ?? [])
            .filter((c) => c.video_count > 0)
            .map((c) => (
              <option key={c.id} value={c.id}>{c.title}</option>
            ))}
        </select>
        <label className="flex items-center gap-2 text-xs text-slate-400">
          起始
          <input
            type="date"
            className="flex-1 px-2 py-1.5 bg-slate-800 rounded text-sm"
            value={draft.dateFrom}
            onChange={(e) => setDraft({ ...draft, dateFrom: e.target.value })}
          />
        </label>
        <label className="flex items-center gap-2 text-xs text-slate-400">
          结束
          <input
            type="date"
            className="flex-1 px-2 py-1.5 bg-slate-800 rounded text-sm"
            value={draft.dateTo}
            onChange={(e) => setDraft({ ...draft, dateTo: e.target.value })}
          />
        </label>
        <div className="sm:col-span-2 lg:col-span-1 flex gap-2">
          <button className="flex-1 px-4 py-2 bg-emerald-700 hover:bg-emerald-600 rounded text-sm">搜索</button>
          <button
            type="button"
            onClick={() => setSearchParams(new URLSearchParams())}
            className="px-3 py-2 bg-slate-700 hover:bg-slate-600 rounded text-sm"
          >清空</button>
        </div>
      </form>

      {!hasAny(submitted) && (
        <div className="p-6 text-slate-400 text-sm">
          支持任一组合: 文件名 / 正文 / 日期范围 / 频道。每个字段都是 ILIKE 模糊匹配。
        </div>
      )}

      {hasAny(submitted) && result.isLoading && (
        <div className="p-6 text-slate-400">搜索中…</div>
      )}

      {hasAny(submitted) && result.data && (
        <>
          <div className="px-6 pt-4 flex items-center gap-3 flex-wrap">
            <span className="text-xs text-slate-500">命中 {all.length} 条</span>
            <SortSelect
              value={submitted.order}
              onChange={changeOrder}
              className="ml-auto px-3 py-1.5 bg-slate-800 rounded text-sm"
            />
          </div>
          <VideoGrid
            videos={all}
            linkTo={(v) => {
              const p = new URLSearchParams();
              if (submitted.text) p.set("text", submitted.text);
              if (submitted.fileName) p.set("file_name", submitted.fileName);
              if (submitted.dateFrom) p.set("date_from", submitted.dateFrom);
              if (submitted.dateTo) p.set("date_to", submitted.dateTo);
              if (submitted.channelID > 0) p.set("ch", String(submitted.channelID));
              if (submitted.order !== DEFAULT_SORT) p.set("order", submitted.order);
              return `/videos/${v.id}?${p}`;
            }}
          />
          <div className="py-6 flex items-center justify-center">
            {result.hasNextPage ? (
              <button
                onClick={() => result.fetchNextPage()}
                disabled={result.isFetchingNextPage}
                className="px-6 py-2 bg-emerald-700 hover:bg-emerald-600 disabled:opacity-50 rounded text-sm"
              >
                {result.isFetchingNextPage ? "加载中…" : `加载下一页 (+${PAGE_SIZE})`}
              </button>
            ) : all.length > 0 ? (
              <span className="text-xs text-slate-500">— 已加载全部 —</span>
            ) : (
              <span className="text-xs text-slate-500">无匹配结果</span>
            )}
          </div>
        </>
      )}
    </div>
  );
}
