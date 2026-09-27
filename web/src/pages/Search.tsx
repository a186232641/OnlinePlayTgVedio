import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, Channel, MediaKindFilter } from "../api/client";
import { MEDIA_PAGE_SIZE, normalizeKind, useMediaPages } from "../api/media";
import { LIST_PAGE_SIZE, useDebounced, usePagedList } from "../api/paged";
import { KindTabs, MediaBrowser } from "../components/MediaBrowser";
import { SortSelect, SortValue, normalizeSort, DEFAULT_SORT } from "../components/SortSelect";
import { ChevronRightIcon, GridIcon, SearchIcon, TopicsIcon } from "../components/icons";
import { EmptyState, LoadingState, MoreFooter, PageHeader, cx } from "../components/ui";

interface Filters {
  text: string;
  fileName: string;
  dateFrom: string; // yyyy-mm-dd
  dateTo: string;
  channelID: number;
  order: SortValue;
  kind: MediaKindFilter;
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
    kind: normalizeKind(p.get("kind")),
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
  if (f.kind) p.set("kind", f.kind);
  return p;
}

function hasAny(f: Filters): boolean {
  return !!(f.text || f.fileName || f.dateFrom || f.dateTo);
}

// The search page has two modes: media (the default — files and captions) and
// topics (?mode=topic — topic titles across every forum group).
export function Search() {
  const [searchParams, setSearchParams] = useSearchParams();
  if (searchParams.get("mode") === "topic") {
    return (
      <div className="space-y-5 p-4 md:p-6">
        <PageHeader title="搜索" meta="按标题搜索所有论坛群组里的话题" actions={<ModeTabs mode="topic" />} />
        <TopicSearch />
      </div>
    );
  }
  return <MediaSearch searchParams={searchParams} setSearchParams={setSearchParams} />;
}

function ModeTabs({ mode }: { mode: "media" | "topic" }) {
  const tabs = [
    { value: "media", label: "搜媒体", Icon: GridIcon, to: "/search" },
    { value: "topic", label: "搜话题", Icon: TopicsIcon, to: "/search?mode=topic" },
  ] as const;
  return (
    <div className="inline-flex rounded-xl bg-gray-100 p-1 dark:bg-white/[0.06]">
      {tabs.map(({ value, label, Icon, to }) => (
        <Link
          key={value}
          to={to}
          replace
          className={cx(
            "inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-theme-xs font-medium transition-colors",
            mode === value
              ? "bg-white text-gray-800 shadow-theme-xs dark:bg-white/[0.08] dark:text-white/90"
              : "text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200",
          )}
        >
          <Icon className="size-4" />
          {label}
        </Link>
      ))}
    </div>
  );
}

interface TopicHit extends Channel {
  parent_title?: string;
}

// TopicSearch finds topics by title across all forum groups. The box writes
// ?q= (replace, so it adds no Back steps) and the request is debounced; the
// list pages with limit/offset like the per-group topic list.
function TopicSearch() {
  const [searchParams, setSearchParams] = useSearchParams();
  const text = searchParams.get("q") ?? "";
  const debounced = useDebounced(text.trim());
  const { query: q, items, total } = usePagedList<TopicHit, { topics: TopicHit[]; has_more?: boolean; total?: number }>({
    key: ["topic-search", debounced],
    path: "/api/topics",
    params: new URLSearchParams(debounced ? { q: debounced } : {}),
    pick: (r) => r.topics,
    keyOf: (t) => t.id,
  });

  return (
    <>
      <div className="card flex flex-wrap items-center gap-3 p-4">
        <div className="relative min-w-[220px] flex-1">
          <SearchIcon className="pointer-events-none absolute left-3.5 top-1/2 size-5 -translate-y-1/2 text-gray-400" />
          <input
            className="field pl-11"
            placeholder="话题标题关键词 — 留空列出全部话题(内容最多的在前)"
            value={text}
            autoFocus
            onChange={(e) => {
              const p = new URLSearchParams(searchParams);
              if (e.target.value) p.set("q", e.target.value);
              else p.delete("q");
              setSearchParams(p, { replace: true });
            }}
          />
        </div>
        {total != null && (
          <span className="text-theme-xs text-gray-500 dark:text-gray-400">
            {debounced ? "命中" : "共"} <span className="font-medium text-gray-700 dark:text-gray-300">{total}</span> 个话题
          </span>
        )}
      </div>

      {q.isLoading ? (
        <LoadingState label="搜索中…" />
      ) : items.length === 0 ? (
        <EmptyState title={debounced ? "没有匹配的话题" : "还没有话题"} />
      ) : (
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2 3xl:grid-cols-3">
          {items.map((t) => {
            const bits = [
              t.video_count > 0 && `${t.video_count.toLocaleString()} 视频`,
              t.photo_count > 0 && `${t.photo_count.toLocaleString()} 图片`,
            ].filter(Boolean) as string[];
            return (
              <Link
                key={t.id}
                to={`/channels/${t.id}`}
                className="card group flex items-center gap-3 p-4 transition-colors hover:border-brand-300 dark:hover:border-brand-500/40"
              >
                <span className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-gray-100 text-gray-400 transition-colors group-hover:bg-brand-500 group-hover:text-white dark:bg-white/[0.06] dark:text-gray-500">
                  <TopicsIcon className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                  {t.parent_title && (
                    <div className="truncate text-theme-xs text-gray-400">{t.parent_title}</div>
                  )}
                  <div className="truncate font-medium text-gray-800 transition-colors group-hover:text-brand-600 dark:text-white/90 dark:group-hover:text-brand-400">
                    {t.title}
                  </div>
                  <div className="mt-0.5 truncate text-theme-xs text-gray-500 dark:text-gray-400">
                    {bits.length > 0 ? bits.join(" · ") : "尚未同步"}
                    {t.topic_closed && " · 已关闭"}
                  </div>
                </div>
                <ChevronRightIcon className="size-5 shrink-0 text-gray-300 transition-colors group-hover:text-brand-500 dark:text-gray-600" />
              </Link>
            );
          })}
        </div>
      )}

      <MoreFooter
        hasNextPage={!!q.hasNextPage}
        isFetchingNextPage={q.isFetchingNextPage}
        fetchNextPage={q.fetchNextPage}
        doneLabel="已加载全部话题"
        loaded={items.length}
        pageSize={LIST_PAGE_SIZE}
      />
    </>
  );
}

function MediaSearch({
  searchParams,
  setSearchParams,
}: {
  searchParams: URLSearchParams;
  setSearchParams: ReturnType<typeof useSearchParams>[1];
}) {
  const submitted = useMemo(() => filtersFromParams(searchParams), [searchParams]);

  const [draft, setDraft] = useState<Filters>(submitted);
  // Re-sync the form whenever the URL changes from outside the form (Back nav,
  // sort change) so the inputs reflect the active search.
  useEffect(() => { setDraft(submitted); }, [searchParams]); // eslint-disable-line react-hooks/exhaustive-deps

  const channels = useQuery<{ channels: Channel[] }>({
    queryKey: ["channels", "all"],
    queryFn: () => api.get("/api/channels/"),
  });

  const { query: result, items: all, sources } = useMediaPages(
    ["search", submitted],
    () => {
      const qs = new URLSearchParams();
      if (submitted.text) qs.set("text", submitted.text);
      if (submitted.fileName) qs.set("file_name", submitted.fileName);
      if (submitted.dateFrom) qs.set("date_from", submitted.dateFrom);
      if (submitted.dateTo) qs.set("date_to", submitted.dateTo);
      if (submitted.channelID > 0) qs.set("channel_id", String(submitted.channelID));
      if (submitted.order !== DEFAULT_SORT) qs.set("order", submitted.order);
      if (submitted.kind) qs.set("kind", submitted.kind);
      return qs;
    },
    { path: "/api/media/search", enabled: hasAny(submitted) },
  );

  // Sort applies immediately to the active search (re-sorts the URL), keeping
  // the rest of the submitted filters untouched.
  const changeOrder = (order: SortValue) =>
    setSearchParams(paramsFromFilters({ ...submitted, order }));

  return (
    <div className="space-y-5 p-4 md:p-6">
      <PageHeader
        title="搜索"
        meta="文件名 / 正文 / 日期范围 / 频道,每个字段都是 ILIKE 模糊匹配 — 视频和图片一起搜"
        actions={<ModeTabs mode="media" />}
      />

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
              .filter((c) => c.video_count > 0 || c.photo_count > 0)
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
            <KindTabs
              value={submitted.kind}
              onChange={(kind) => setSearchParams(paramsFromFilters({ ...submitted, kind }))}
            />
            <SortSelect
              value={submitted.order}
              onChange={changeOrder}
              className="field field-select field-sm ml-auto w-auto"
            />
          </div>

          <MediaBrowser
            items={all}
            hasMore={!!result.hasNextPage}
            loadingMore={result.isFetchingNextPage}
            onLoadMore={result.fetchNextPage}
            sources={sources}
            emptyLabel="无匹配结果"
            linkTo={(m) => {
              const p = new URLSearchParams();
              if (submitted.text) p.set("text", submitted.text);
              if (submitted.fileName) p.set("file_name", submitted.fileName);
              if (submitted.dateFrom) p.set("date_from", submitted.dateFrom);
              if (submitted.dateTo) p.set("date_to", submitted.dateTo);
              if (submitted.channelID > 0) p.set("ch", String(submitted.channelID));
              if (submitted.order !== DEFAULT_SORT) p.set("order", submitted.order);
              if (submitted.kind) p.set("kind", submitted.kind);
              return `/videos/${m.id}?${p}`;
            }}
          />

          <MoreFooter
            hasNextPage={!!result.hasNextPage}
            isFetchingNextPage={result.isFetchingNextPage}
            fetchNextPage={result.fetchNextPage}
            doneLabel="已加载全部"
            loaded={all.length}
            pageSize={MEDIA_PAGE_SIZE}
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
