import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useInfiniteQuery } from "@tanstack/react-query";

import { api, ApiError, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";
import { SortSelect, SortValue, normalizeSort, DEFAULT_SORT } from "../components/SortSelect";
import { AlertStrip, LoadingState, MoreFooter, PageHeader } from "../components/ui";

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
      <div className="p-4 md:p-6">
        <AlertStrip title="加载收藏失败">
          <pre className="whitespace-pre-wrap font-mono">
            {`status: ${err.status}\ncode: ${err.code}\nmessage: ${err.message}`}
          </pre>
        </AlertStrip>
      </div>
    );
  }

  return (
    <div className="space-y-5 p-4 md:p-6">
      <PageHeader
        title="收藏"
        meta={
          <>
            {filtered ? "命中" : "共"}{" "}
            <span className="font-medium text-gray-700 dark:text-gray-300">{all.length}</span> 条收藏
            {q.hasNextPage ? " (还有更多)" : ""} · 收藏的视频会被固定在磁盘缓存里
          </>
        }
      />

      <form
        onSubmit={(e) => { e.preventDefault(); setSearchParams(paramsFromFilters(draft)); }}
        className="card grid gap-4 p-4 sm:grid-cols-2 lg:grid-cols-4"
      >
        <label className="flex flex-col gap-1.5 sm:col-span-2">
          <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">
            文件名 (file_name)
          </span>
          <input
            className="field"
            placeholder="按文件名过滤收藏…"
            value={draft.fileName}
            onChange={(e) => setDraft({ ...draft, fileName: e.target.value })}
          />
        </label>
        <label className="flex flex-col gap-1.5">
          <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">起始日期</span>
          <input
            type="date"
            className="field"
            value={draft.dateFrom}
            onChange={(e) => setDraft({ ...draft, dateFrom: e.target.value })}
          />
        </label>
        <label className="flex flex-col gap-1.5">
          <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">结束日期</span>
          <input
            type="date"
            className="field"
            value={draft.dateTo}
            onChange={(e) => setDraft({ ...draft, dateTo: e.target.value })}
          />
        </label>
        <div className="flex flex-wrap items-center gap-2 sm:col-span-2 lg:col-span-4">
          <button className="btn btn-primary">搜索收藏</button>
          {filtered && (
            <button
              type="button"
              onClick={() =>
                setSearchParams(
                  paramsFromFilters({ fileName: "", dateFrom: "", dateTo: "", order: submitted.order }),
                )
              }
              className="btn btn-outline"
            >清空</button>
          )}
          <SortSelect
            value={submitted.order}
            onChange={changeOrder}
            className="field field-select ml-auto w-auto"
          />
        </div>
      </form>

      {q.isLoading ? (
        <LoadingState />
      ) : (
        <VideoGrid
          videos={all}
          emptyLabel={filtered ? "无匹配收藏" : "暂无收藏 — 播放页点「收藏」即可加入"}
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

      <MoreFooter
        hasNextPage={!!q.hasNextPage}
        isFetchingNextPage={q.isFetchingNextPage}
        fetchNextPage={q.fetchNextPage}
        doneLabel="已加载全部"
        loaded={all.length}
        pageSize={PAGE_SIZE}
      />
    </div>
  );
}
