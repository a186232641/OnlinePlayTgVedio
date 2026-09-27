import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";

import { api, ApiError, FavoriteSource, MediaItem, MediaKindFilter, MediaSource } from "../api/client";
import { MEDIA_PAGE_SIZE, normalizeKind, useMediaPages } from "../api/media";
import { KindTabs, MediaBrowser } from "../components/MediaBrowser";
import { SortSelect, SortValue, normalizeSort, FAV_DEFAULT_SORT, FAV_SORT_OPTIONS } from "../components/SortSelect";
import { ChevronLeftIcon, GridIcon, ImageIcon, PlayIcon, TopicsIcon } from "../components/icons";
import { AlertStrip, EmptyState, LoadingState, MoreFooter, PageHeader, cx } from "../components/ui";

// View is how the favorites page is laid out: every item, or one card per
// channel/topic the items came from ("sources"). Opening a card is the item
// view scoped by channelId.
type View = "" | "sources";

interface Filters {
  fileName: string;
  dateFrom: string; // yyyy-mm-dd
  dateTo: string;
  order: SortValue;
  kind: MediaKindFilter;
  view: View;
  channelId: string; // one source's favorites; "" = all
}

// URL is the source of truth so returning from a video restores the filtered,
// sorted favorites view.
function filtersFromParams(p: URLSearchParams): Filters {
  return {
    fileName: p.get("file_name") ?? "",
    dateFrom: p.get("date_from") ?? "",
    dateTo: p.get("date_to") ?? "",
    order: normalizeSort(p.get("order"), FAV_SORT_OPTIONS, FAV_DEFAULT_SORT),
    kind: normalizeKind(p.get("kind")),
    // A source drill-down is an item view, whatever ?view says.
    view: p.get("view") === "sources" && !p.get("channel_id") ? "sources" : "",
    channelId: p.get("channel_id") ?? "",
  };
}

function paramsFromFilters(f: Filters): URLSearchParams {
  const p = new URLSearchParams();
  if (f.fileName) p.set("file_name", f.fileName);
  if (f.dateFrom) p.set("date_from", f.dateFrom);
  if (f.dateTo) p.set("date_to", f.dateTo);
  if (f.order !== FAV_DEFAULT_SORT) p.set("order", f.order);
  if (f.kind) p.set("kind", f.kind);
  if (f.view) p.set("view", f.view);
  if (f.channelId) p.set("channel_id", f.channelId);
  return p;
}

// dayLabel turns a timestamp into a section heading in the viewer's local time:
// 今天 / 昨天 / 2026-09-14 (周一).
const WEEKDAYS = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];
function dayLabel(iso?: string): string | null {
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

// groupFor picks the day to group by for the active sort: favorite time when
// sorted by it, publish date when sorted by date, nothing for name sorts (a
// name order scatters any one day across the whole list).
function groupFor(order: SortValue): ((m: MediaItem) => string | null) | undefined {
  if (order.startsWith("fav")) return (m) => dayLabel(m.favorited_at);
  if (order.startsWith("date")) return (m) => dayLabel(m.date) ?? "未知日期";
  return undefined;
}

export function Favorites() {
  const [searchParams, setSearchParams] = useSearchParams();
  const submitted = useMemo(() => filtersFromParams(searchParams), [searchParams]);

  const [draft, setDraft] = useState<Filters>(submitted);
  useEffect(() => { setDraft(submitted); }, [searchParams]); // eslint-disable-line react-hooks/exhaustive-deps

  const bySource = submitted.view === "sources";
  const { query: q, items, sources } = useMediaPages(
    ["favorites", submitted],
    () => paramsFromFilters(submitted),
    { path: "/api/favorites/", enabled: !bySource },
  );

  const filtered = !!(submitted.fileName || submitted.dateFrom || submitted.dateTo);
  const groupBy = useMemo(() => groupFor(submitted.order), [submitted.order]);

  const patch = (next: Partial<Filters>) =>
    setSearchParams(paramsFromFilters({ ...submitted, ...next }));

  const viewTabs = (
    <ViewTabs
      value={submitted.view}
      onChange={(view) => patch({ view, channelId: "" })}
    />
  );

  if (bySource) {
    return (
      <div className="space-y-5 p-4 md:p-6">
        <PageHeader
          title="收藏"
          meta="按话题 / 频道归类 — 同一话题收藏的视频和图片合并成一张卡片"
          actions={viewTabs}
        />
        <div className="flex flex-wrap items-center gap-2">
          <KindTabs value={submitted.kind} onChange={(kind) => patch({ kind })} />
        </div>
        <SourceCards
          kind={submitted.kind}
          linkTo={(id) => `/favorites?${paramsFromFilters({ ...submitted, view: "", channelId: String(id) })}`}
        />
      </div>
    );
  }

  // In a drill-down the page's own items name the source; fall back to the id
  // until the first page lands.
  const src: MediaSource | undefined = submitted.channelId ? sources[submitted.channelId] : undefined;

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

  // Videos carry the filter context into the player so prev/next walk the same
  // filtered favorites list.
  const linkTo = (m: MediaItem) => {
    const p = new URLSearchParams({ fav: "1" });
    if (submitted.fileName) p.set("file_name", submitted.fileName);
    if (submitted.dateFrom) p.set("date_from", submitted.dateFrom);
    if (submitted.dateTo) p.set("date_to", submitted.dateTo);
    // Always explicit: the player derives its playlist order (and the reverse
    // order for "加载上一页") from this, and must not guess the favorites default.
    p.set("order", submitted.order);
    // Not used by the playlist (always videos) — carried so "返回收藏" restores the tab.
    if (submitted.kind) p.set("kind", submitted.kind);
    // Scopes the playlist to the opened source, and "返回收藏" back to it.
    if (submitted.channelId) p.set("channel_id", submitted.channelId);
    return `/videos/${m.id}?${p}`;
  };

  return (
    <div className="space-y-5 p-4 md:p-6">
      {submitted.channelId && (
        <Link
          to={`/favorites?${paramsFromFilters({ ...submitted, view: "sources", channelId: "" })}`}
          className="inline-flex items-center gap-1 text-theme-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
        >
          <ChevronLeftIcon className="size-4" />
          返回按话题
        </Link>
      )}
      <PageHeader
        title={submitted.channelId ? <>收藏 · {src ? sourceTitle(src) : `#${submitted.channelId}`}</> : "收藏"}
        actions={
          submitted.channelId ? (
            <Link to={`/channels/${submitted.channelId}`} className="btn btn-outline btn-sm">
              打开{src?.dialog_kind === "topic" ? "话题" : "频道"}
            </Link>
          ) : (
            viewTabs
          )
        }
        meta={
          <>
            {filtered ? "命中" : "共"}{" "}
            <span className="font-medium text-gray-700 dark:text-gray-300">{items.length}</span> 条收藏
            {q.hasNextPage ? " (还有更多)" : ""} · 收藏的视频和图片会被固定在磁盘缓存里
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
          <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">发布日期 从</span>
          <input
            type="date"
            className="field"
            value={draft.dateFrom}
            onChange={(e) => setDraft({ ...draft, dateFrom: e.target.value })}
          />
        </label>
        <label className="flex flex-col gap-1.5">
          <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">发布日期 到</span>
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
                  paramsFromFilters({
                    fileName: "",
                    dateFrom: "",
                    dateTo: "",
                    order: submitted.order,
                    kind: submitted.kind,
                    view: submitted.view,
                    channelId: submitted.channelId,
                  }),
                )
              }
              className="btn btn-outline"
            >清空</button>
          )}
          <KindTabs value={submitted.kind} onChange={(kind) => patch({ kind })} />
          <SortSelect
            value={submitted.order}
            onChange={(order) => patch({ order })}
            options={FAV_SORT_OPTIONS}
            className="field field-select ml-auto w-auto"
          />
        </div>
      </form>

      <MediaBrowser
        items={items}
        isLoading={q.isLoading}
        hasMore={!!q.hasNextPage}
        loadingMore={q.isFetchingNextPage}
        onLoadMore={q.fetchNextPage}
        linkTo={linkTo}
        sources={submitted.channelId ? undefined : sources}
        groupBy={groupBy}
        emptyLabel={filtered ? "无匹配收藏" : "暂无收藏 — 播放页或图片查看器里点「收藏」即可加入"}
      />

      <MoreFooter
        hasNextPage={!!q.hasNextPage}
        isFetchingNextPage={q.isFetchingNextPage}
        fetchNextPage={q.fetchNextPage}
        doneLabel="已加载全部"
        loaded={items.length}
        pageSize={MEDIA_PAGE_SIZE}
      />
    </div>
  );
}

function sourceTitle(s: MediaSource): string {
  return s.dialog_kind === "topic" && s.parent_title ? `${s.parent_title} › ${s.title}` : s.title;
}

const VIEWS: { value: View; label: string; Icon: typeof GridIcon }[] = [
  { value: "", label: "全部收藏", Icon: GridIcon },
  { value: "sources", label: "按话题", Icon: TopicsIcon },
];

function ViewTabs({ value, onChange }: { value: View; onChange: (v: View) => void }) {
  return (
    <div className="inline-flex rounded-xl bg-gray-100 p-1 dark:bg-white/[0.06]">
      {VIEWS.map(({ value: v, label, Icon }) => (
        <button
          key={v || "all"}
          type="button"
          onClick={() => onChange(v)}
          className={cx(
            "inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-theme-xs font-medium transition-colors",
            value === v
              ? "bg-white text-gray-800 shadow-theme-xs dark:bg-white/[0.08] dark:text-white/90"
              : "text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200",
          )}
        >
          <Icon className="size-4" />
          {label}
        </button>
      ))}
    </div>
  );
}

function Cover({ g }: { g: FavoriteSource }) {
  const [failed, setFailed] = useState(false);
  if (failed) {
    return (
      <div className="flex size-full items-center justify-center bg-gray-100 text-gray-300 dark:bg-white/[0.04] dark:text-gray-600">
        {g.cover_kind === "video" ? <PlayIcon className="size-8" /> : <ImageIcon className="size-8" />}
      </div>
    );
  }
  return (
    <img
      src={g.cover_thumb_url}
      alt=""
      loading="lazy"
      decoding="async"
      onError={() => setFailed(true)}
      className="size-full bg-gray-100 object-cover transition-transform duration-200 group-hover:scale-[1.03] dark:bg-white/[0.04]"
    />
  );
}

// SourceCards is the by-source favorites view: one card per channel or topic,
// newest-favorited first, each opening that source's favorites.
function SourceCards({ kind, linkTo }: { kind: MediaKindFilter; linkTo: (channelId: number) => string }) {
  const q = useQuery<{ items: FavoriteSource[] }>({
    queryKey: ["favorites", "sources", kind],
    queryFn: () => api.get(`/api/favorites/sources${kind ? `?kind=${kind}` : ""}`),
  });

  if (q.error) {
    const err = q.error as ApiError;
    return <AlertStrip title="加载收藏失败">{err.message}</AlertStrip>;
  }
  if (q.isLoading) return <LoadingState />;
  const groups = q.data?.items ?? [];
  if (groups.length === 0) return <EmptyState title="暂无收藏" />;

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 2xl:grid-cols-5 3xl:grid-cols-6">
      {groups.map((g) => {
        const s = g.source;
        const isTopic = s.dialog_kind === "topic";
        return (
          <Link
            key={s.id}
            to={linkTo(s.id)}
            className="card group flex flex-col overflow-hidden p-0 transition-colors hover:border-brand-300 dark:hover:border-brand-500/40"
          >
            <div className="relative aspect-[4/3] w-full overflow-hidden rounded-t-2xl">
              <Cover g={g} />
              <span className="absolute bottom-1.5 right-1.5 rounded-md bg-black/65 px-1.5 py-0.5 text-theme-xs tabular-nums text-white">
                {(g.videos + g.photos).toLocaleString()} 条
              </span>
            </div>
            <div className="flex min-w-0 flex-1 flex-col gap-1 p-3">
              {isTopic && s.parent_title && (
                <div className="truncate text-theme-xs text-gray-400" title={s.parent_title}>
                  {s.parent_title}
                </div>
              )}
              <div className="line-clamp-2 break-all text-theme-sm font-medium leading-snug text-gray-800 transition-colors group-hover:text-brand-600 dark:text-white/90 dark:group-hover:text-brand-400">
                {isTopic && <TopicsIcon className="mr-1 inline size-3.5 align-[-2px] text-gray-400" />}
                {s.title}
              </div>
              <div className="mt-auto flex flex-wrap items-center gap-x-2 text-theme-xs text-gray-500 dark:text-gray-400">
                {g.videos > 0 && <span>{g.videos.toLocaleString()} 视频</span>}
                {g.photos > 0 && <span>{g.photos.toLocaleString()} 图片</span>}
                <span className="ml-auto">{dayLabel(g.last_favorited_at)}</span>
              </div>
            </div>
          </Link>
        );
      })}
    </div>
  );
}
