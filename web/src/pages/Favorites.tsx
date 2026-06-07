import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useInfiniteQuery } from "@tanstack/react-query";

import { api, ApiError, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";
import { SortSelect, SortValue, normalizeSort, DEFAULT_SORT } from "../components/SortSelect";

interface Page { videos: Video[] }

const PAGE_SIZE = 200;

interface Filters {
  fileName: string;
  dateFrom: string; // yyyy-mm-dd
  dateTo: string;
  order: SortValue;
}

// URL is the source of truth so returning from a video restores the filtered,
// sorted favorites view.
function filtersFromParams(p: URLSearchParams): Filters {
  return {
    fileName: p.get("file_name") ?? "",
    dateFrom: p.get("date_from") ?? "",
    dateTo: p.get("date_to") ?? "",
    order: normalizeSort(p.get("order")),
  };
}

function paramsFromFilters(f: Filters): URLSearchParams {
  const p = new URLSearchParams();
  if (f.fileName) p.set("file_name", f.fileName);
  if (f.dateFrom) p.set("date_from", f.dateFrom);
  if (f.dateTo) p.set("date_to", f.dateTo);
  if (f.order !== DEFAULT_SORT) p.set("order", f.order);
  return p;
}

export function Favorites() {
  const [searchParams, setSearchParams] = useSearchParams();
  const submitted = useMemo(() => filtersFromParams(searchParams), [searchParams]);

  const [draft, setDraft] = useState<Filters>(submitted);
  useEffect(() => { setDraft(submitted); }, [searchParams]); // eslint-disable-line react-hooks/exhaustive-deps

  const q = useInfiniteQuery<Page>({
    queryKey: ["favorites", submitted],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      const cursor = pageParam as number;
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE) });
      if (submitted.fileName) qs.set("file_name", submitted.fileName);
      if (submitted.dateFrom) qs.set("date_from", submitted.dateFrom);
      if (submitted.dateTo) qs.set("date_to", submitted.dateTo);
      if (submitted.order !== DEFAULT_SORT) qs.set("order", submitted.order);
      if (cursor > 0) qs.set("offset_id", String(cursor));
      return api.get<Page>(`/api/favorites/?${qs}`);
    },
    getNextPageParam: (last) =>
      last.videos.length < PAGE_SIZE ? undefined : last.videos[last.videos.length - 1]?.id,
  });

  const all = useMemo<Video[]>(() => q.data?.pages.flatMap((p) => p.videos) ?? [], [q.data]);
  const filtered = !!(submitted.fileName || submitted.dateFrom || submitted.dateTo);

  const changeOrder = (order: SortValue) =>
    setSearchParams(paramsFromFilters({ ...submitted, order }));

  if (q.error) {
    const err = q.error as ApiError;
    return (
      <div className="p-6 space-y-2">
        <div className="text-red-400">加载收藏失败</div>
        <pre className="text-xs text-slate-400 whitespace-pre-wrap">
          {`status: ${err.status}\ncode: ${err.code}\nmessage: ${err.message}`}
        </pre>
      </div>
    );
  }

  return (
    <div>
      <form
        onSubmit={(e) => { e.preventDefault(); setSearchParams(paramsFromFilters(draft)); }}
        className="p-4 border-b border-slate-800 grid gap-2 sm:grid-cols-2 lg:grid-cols-4 items-center"
      >
        <input
          className="px-3 py-2 bg-slate-800 rounded text-sm sm:col-span-2"
          placeholder="文件名(file_name)…"
          value={draft.fileName}
          onChange={(e) => setDraft({ ...draft, fileName: e.target.value })}
        />
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
        <div className="flex gap-2 sm:col-span-2 lg:col-span-4">
          <button className="px-4 py-2 bg-emerald-700 hover:bg-emerald-600 rounded text-sm">搜索收藏</button>
          {filtered && (
            <button
              type="button"
              onClick={() => setSearchParams(paramsFromFilters({ fileName: "", dateFrom: "", dateTo: "", order: submitted.order }))}
              className="px-3 py-2 bg-slate-700 hover:bg-slate-600 rounded text-sm"
            >清空</button>
          )}
          <SortSelect
            value={submitted.order}
            onChange={changeOrder}
            className="ml-auto px-3 py-2 bg-slate-800 rounded text-sm"
          />
        </div>
      </form>

      <div className="px-6 pt-4 text-xs text-slate-500">
        {filtered ? `命中 ${all.length} 条收藏` : `共 ${all.length} 条收藏`}
        {q.hasNextPage ? " (还有更多)" : ""}
      </div>

      {q.isLoading ? (
        <div className="p-6 text-slate-400">加载中…</div>
      ) : (
        <VideoGrid
          videos={all}
          linkTo={(v) => {
            const p = new URLSearchParams({ fav: "1" });
            if (submitted.fileName) p.set("file_name", submitted.fileName);
            if (submitted.dateFrom) p.set("date_from", submitted.dateFrom);
            if (submitted.dateTo) p.set("date_to", submitted.dateTo);
            if (submitted.order !== DEFAULT_SORT) p.set("order", submitted.order);
            return `/videos/${v.id}?${p}`;
          }}
        />
      )}

      <div className="py-6 flex items-center justify-center">
        {q.hasNextPage ? (
          <button
            onClick={() => q.fetchNextPage()}
            disabled={q.isFetchingNextPage}
            className="px-6 py-2 bg-emerald-700 hover:bg-emerald-600 disabled:opacity-50 rounded text-sm"
          >
            {q.isFetchingNextPage ? "加载中…" : `加载下一页 (+${PAGE_SIZE})`}
          </button>
        ) : all.length > 0 ? (
          <span className="text-xs text-slate-500">— 已加载全部 —</span>
        ) : !q.isLoading ? (
          <span className="text-xs text-slate-500">{filtered ? "无匹配收藏" : "暂无收藏"}</span>
        ) : null}
      </div>
    </div>
  );
}
