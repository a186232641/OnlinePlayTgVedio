import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";

import { api, Channel, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";
import { SortSelect, SortValue, normalizeSort, DEFAULT_SORT } from "../components/SortSelect";
import { EmptyState, LoadingState, MoreFooter, PageHeader } from "../components/ui";

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
    <div className="space-y-5 p-4 md:p-6">
      <PageHeader title="搜索" meta="文件名 / 正文 / 日期范围 / 频道,每个字段都是 ILIKE 模糊匹配" />

      <form
        onSubmit={(e) => { e.preventDefault(); setSearchParams(paramsFromFilters(draft)); }}
        className="card grid gap-4 p-4 sm:grid-cols-2 lg:grid-cols-4"
      >
        <Field label="文件名 (file_name)" className="sm:col-span-2">
          <input
            className="field"
            placeholder="例如 anchor-2024…"
            value={draft.fileName}
            onChange={(e) => setDraft({ ...draft, fileName: e.target.value })}
          />
        </Field>
        <Field label="正文 / caption (text)" className="sm:col-span-2">
          <input
            className="field"
            placeholder="消息正文关键词…"
            value={draft.text}
            onChange={(e) => setDraft({ ...draft, text: e.target.value })}
          />
        </Field>
        <Field label="频道">
          <select
            className="field field-select"
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
        </Field>
        <Field label="起始日期">
          <input
            type="date"
            className="field"
            value={draft.dateFrom}
            onChange={(e) => setDraft({ ...draft, dateFrom: e.target.value })}
          />
        </Field>
        <Field label="结束日期">
          <input
            type="date"
            className="field"
            value={draft.dateTo}
            onChange={(e) => setDraft({ ...draft, dateTo: e.target.value })}
          />
        </Field>
        <div className="flex items-end gap-2">
          <button className="btn btn-primary flex-1">搜索</button>
          <button
            type="button"
            onClick={() => setSearchParams(new URLSearchParams())}
            className="btn btn-outline"
          >清空</button>
        </div>
      </form>

      {!hasAny(submitted) && (
        <EmptyState
          title="填入任一条件开始搜索"
          hint="支持任意组合:文件名 / 正文 / 日期范围 / 频道。搜索条件会写进 URL,从视频返回时会原样恢复。"
        />
      )}

      {hasAny(submitted) && result.isLoading && <LoadingState label="搜索中…" />}

      {hasAny(submitted) && result.data && (
        <>
          <div className="flex flex-wrap items-center gap-3">
            <span className="text-theme-xs text-gray-500 dark:text-gray-400">
              命中 <span className="font-medium text-gray-700 dark:text-gray-300">{all.length}</span> 条
              {result.hasNextPage ? " (还有更多)" : ""}
            </span>
            <SortSelect
              value={submitted.order}
              onChange={changeOrder}
              className="field field-select field-sm ml-auto w-auto"
            />
          </div>

          <VideoGrid
            videos={all}
            emptyLabel="无匹配结果"
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

          <MoreFooter
            hasNextPage={!!result.hasNextPage}
            isFetchingNextPage={result.isFetchingNextPage}
            fetchNextPage={result.fetchNextPage}
            doneLabel="已加载全部"
            loaded={all.length}
            pageSize={PAGE_SIZE}
          />
        </>
      )}
    </div>
  );
}

// Field pairs a 12px label with its control — the form language the design
// specifies (label above, 44px control, hairline border).
function Field({
  label, className, children,
}: {
  label: string;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <label className={"flex flex-col gap-1.5 " + (className ?? "")}>
      <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">{label}</span>
      {children}
    </label>
  );
}
