import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { api, Channel, MediaItem, MediaKindFilter, Streamer, SyncState, Topic } from "../api/client";
import { MEDIA_PAGE_SIZE, useMediaPages } from "../api/media";
import { KindTabs, MediaBrowser } from "../components/MediaBrowser";
import { SortSelect, SortValue, DEFAULT_SORT } from "../components/SortSelect";
import { ChevronLeftIcon, ChevronRightIcon, RefreshIcon, SearchIcon, TopicsIcon } from "../components/icons";
import { EmptyState, LoadingState, MoreFooter, PageHeader, Toggle } from "../components/ui";

interface ChannelResp { channel: Channel }
interface StreamersResp { streamers: Streamer[] }
interface TopicsResp { topics: Topic[] }

export function ChannelDetail() {
  const { id } = useParams();
  const qc = useQueryClient();

  const channelQ = useQuery<ChannelResp>({
    queryKey: ["channel", id],
    queryFn: () => api.get(`/api/channels/${id}`),
    enabled: !!id,
  });
  const channel = channelQ.data?.channel;
  const isForum = !!channel?.is_forum;
  const isTopic = channel?.dialog_kind === "topic";
  const grouped = !!channel?.group_by_streamer;

  // null = streamer-list view; a string (possibly "") = that streamer's videos.
  const [selected, setSelected] = useState<string | null>(null);

  const toggleGroup = useMutation({
    mutationFn: (val: boolean) =>
      api.patch<ChannelResp>(`/api/channels/${id}`, { group_by_streamer: val }),
    onSuccess: (resp) => {
      qc.setQueryData(["channel", id], resp);
      setSelected(null);
    },
    onError: (e: Error) => alert(`切换失败: ${e.message}`),
  });

  const counts = [
    (channel?.video_count ?? 0) > 0 && `${(channel?.video_count ?? 0).toLocaleString()} 个视频`,
    (channel?.photo_count ?? 0) > 0 && `${(channel?.photo_count ?? 0).toLocaleString()} 张图片`,
    isForum && `${(channel?.topic_count ?? 0).toLocaleString()} 个话题`,
  ].filter(Boolean) as string[];

  return (
    <div className="space-y-5 p-4 md:p-6">
      {isTopic && channel?.parent_channel_id && (
        <Link
          to={`/channels/${channel.parent_channel_id}`}
          className="inline-flex items-center gap-1 text-theme-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
        >
          <ChevronLeftIcon className="size-4" />
          返回话题列表
        </Link>
      )}

      <PageHeader
        title={channel?.title ?? "频道"}
        meta={
          <>
            {counts.length > 0 ? counts.join(" · ") : "暂无内容"}
            {channel?.username && ` · @${channel.username}`}
            {isTopic && " · 话题"}
          </>
        }
        actions={
          !isForum && (
            <Toggle
              label="按主播分组"
              title="按文件名里的主播名把视频分组展示"
              checked={grouped}
              disabled={!channel || toggleGroup.isPending}
              onChange={(v) => toggleGroup.mutate(v)}
            />
          )
        }
      />

      {isForum && <TopicList id={id!} />}
      {!isForum && !grouped && <MediaView id={id!} channel={channel} />}
      {!isForum && grouped && selected === null && <StreamerList id={id!} onPick={setSelected} />}
      {!isForum && grouped && selected !== null && (
        <StreamerVideos id={id!} streamer={selected} onBack={() => setSelected(null)} />
      )}
    </div>
  );
}

// TopicList is what a forum group shows instead of a media list: its content
// all lives one level down, in the topics.
//
// Each topic syncs independently — a group can hold dozens of topics with
// hundreds of thousands of messages each, so "sync the whole group" is the
// batch option, not the only one. Live progress for every row comes from the
// topics endpoint itself (one poll, not one per row).
function TopicList({ id }: { id: string }) {
  const qc = useQueryClient();
  const [filter, setFilter] = useState("");

  const q = useQuery<TopicsResp>({
    queryKey: ["channel", id, "topics"],
    queryFn: () => api.get(`/api/channels/${id}/topics`),
    // Poll only while something is actually running.
    refetchInterval: (query) =>
      (query.state.data?.topics ?? []).some((t) => t.sync?.running) ? 2000 : false,
  });

  const refresh = useMutation({
    mutationFn: () => api.post<{ topics: number }>(`/api/channels/${id}/topics/refresh`),
    onSuccess: (resp) => {
      qc.invalidateQueries({ queryKey: ["channel", id] });
      alert(`已拉取 ${resp.topics} 个话题`);
    },
    onError: (e: Error) => alert(`拉取话题失败: ${e.message}`),
  });

  // Group-level sync: refresh the topic list, then walk every topic in turn.
  const syncAll = useMutation({
    mutationFn: () => api.post<SyncState>(`/api/channels/${id}/sync`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["channel", id, "topics"] }),
    onError: (e: Error) => alert(`同步启动失败: ${e.message}`),
  });

  const list = q.data?.topics ?? [];
  const visible = filter
    ? list.filter((t) => t.title.toLowerCase().includes(filter.toLowerCase()))
    : list;
  const anyRunning = list.some((t) => t.sync?.running);

  if (q.isLoading) return <LoadingState />;

  return (
    <div className="space-y-5">
      <div className="card flex flex-wrap items-center gap-3 p-4">
        <div className="relative min-w-[220px] flex-1">
          <SearchIcon className="pointer-events-none absolute left-3.5 top-1/2 size-5 -translate-y-1/2 text-gray-400" />
          <input
            className="field pl-11"
            placeholder="过滤话题…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </div>
        <span className="badge badge-gray">{list.length} 个话题</span>
        <button
          onClick={() => refresh.mutate()}
          disabled={refresh.isPending}
          className="btn btn-outline btn-sm"
          title="从 Telegram 重新枚举该群组的话题(只拉列表,不拉消息)"
        >
          <RefreshIcon className="size-4" />
          {refresh.isPending ? "拉取中…" : "刷新话题"}
        </button>
        <button
          onClick={() => syncAll.mutate()}
          disabled={syncAll.isPending || anyRunning}
          className="btn btn-primary btn-sm"
          title="刷新话题列表后,逐个话题同步消息历史 — 话题多时很慢,建议按需单个同步"
        >
          <RefreshIcon className="size-4" />
          同步全部话题
        </button>
      </div>

      {list.length === 0 ? (
        <EmptyState
          title="还没有话题"
          hint="点「刷新话题」从 Telegram 拉取该群组的话题列表,然后对感兴趣的话题单独点「同步」把里面的图片和视频入库。"
        />
      ) : visible.length === 0 ? (
        <EmptyState title="没有匹配的话题" />
      ) : (
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2 3xl:grid-cols-3">
          {visible.map((t) => (
            <TopicCard key={t.id} topic={t} />
          ))}
        </div>
      )}
    </div>
  );
}

// TopicCard: the body navigates into the topic, the controls act on it in place.
function TopicCard({ topic }: { topic: Topic }) {
  const qc = useQueryClient();
  const running = !!topic.sync?.running;

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["channel", String(topic.parent_channel_id), "topics"] });
  };

  const sync = useMutation({
    mutationFn: () => api.post<SyncState>(`/api/channels/${topic.id}/sync`),
    onSuccess: invalidate,
    onError: (e: Error) => alert(`同步启动失败: ${e.message}`),
  });
  const autoSync = useMutation({
    mutationFn: (val: boolean) => api.patch(`/api/channels/${topic.id}`, { auto_sync: val }),
    onSuccess: invalidate,
    onError: (e: Error) => alert(`切换自动同步失败: ${e.message}`),
  });

  const bits = [
    topic.video_count > 0 && `${topic.video_count.toLocaleString()} 视频`,
    topic.photo_count > 0 && `${topic.photo_count.toLocaleString()} 图片`,
  ].filter(Boolean) as string[];
  const err = topic.sync?.last_error;
  const upToDate = !!err?.startsWith("已是最新");

  return (
    <div className="card flex flex-col gap-3 p-4">
      <div className="flex items-center gap-3">
        <Link to={`/channels/${topic.id}`} className="group flex min-w-0 flex-1 items-center gap-3">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-gray-100 text-gray-400 transition-colors group-hover:bg-brand-500 group-hover:text-white dark:bg-white/[0.06] dark:text-gray-500">
            <TopicsIcon className="size-4" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium text-gray-800 transition-colors group-hover:text-brand-600 dark:text-white/90 dark:group-hover:text-brand-400">
              {topic.title}
            </div>
            <div className="mt-0.5 truncate text-theme-xs text-gray-500 dark:text-gray-400">
              {bits.length > 0 ? bits.join(" · ") : "尚未同步"}
              {topic.topic_closed && " · 已关闭"}
            </div>
          </div>
          <ChevronRightIcon className="size-5 shrink-0 text-gray-300 transition-colors group-hover:text-brand-500 dark:text-gray-600" />
        </Link>
      </div>

      {running && (
        <div className="text-theme-xs text-warning-600 dark:text-warning-400">
          同步中:已遍历 {topic.sync?.walked ?? 0} · 视频 {topic.sync?.videos ?? 0} · 图片{" "}
          {topic.sync?.photos ?? 0} · 跳过 {topic.sync?.skipped ?? 0}
        </div>
      )}
      {!running && err && (
        <div
          className={
            "truncate text-theme-xs " +
            (upToDate
              ? "text-blue-light-600 dark:text-blue-light-400"
              : "text-error-600 dark:text-error-400")
          }
          title={err}
        >
          {upToDate ? err : `上次同步: ${err}`}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2 border-t border-gray-200 pt-3 dark:border-gray-800">
        <button
          onClick={() => sync.mutate()}
          disabled={running || sync.isPending}
          className="btn btn-primary btn-sm"
          title="只同步这个话题的消息历史(增量 + 回填,可中断续传)"
        >
          <RefreshIcon className="size-4" />
          {running ? "同步中…" : "同步"}
        </button>
        <Toggle
          size="sm"
          label="自动同步"
          checked={topic.auto_sync}
          disabled={autoSync.isPending}
          onChange={(v) => autoSync.mutate(v)}
          title="是否纳入后台定时同步(手动同步不受此开关影响)"
        />
      </div>
    </div>
  );
}

// MediaView is the default channel/topic view: videos and images interleaved by
// date, with a kind filter, free-text search and sorting.
function MediaView({ id, channel }: { id: string; channel?: Channel }) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const [order, setOrder] = useState<SortValue>(DEFAULT_SORT);
  const [kind, setKind] = useState<MediaKindFilter>("");

  const { query: q, items, totalVideos, totalPhotos } = useMediaPages(
    ["channel", id, "media", query, order, kind],
    () => {
      const qs = new URLSearchParams();
      if (query) qs.set("q", query);
      if (order !== DEFAULT_SORT) qs.set("order", order);
      if (kind) qs.set("kind", kind);
      return qs;
    },
    { path: `/api/channels/${id}/media` },
  );

  // A topic (or channel) that has never been synced holds nothing yet — say so
  // and offer the one action that fixes it, instead of a bare "empty".
  const neverSynced = !!channel && !channel.last_indexed_at;
  const sync = useMutation({
    mutationFn: () => api.post<SyncState>(`/api/channels/${id}/sync`),
    onSuccess: () => {
      alert("已开始同步,进度可以在 TG 账号管理页或上级话题列表里看到。");
      qc.invalidateQueries({ queryKey: ["channel", id] });
    },
    onError: (e: Error) => alert(`同步启动失败: ${e.message}`),
  });

  const linkTo = (m: MediaItem) => {
    const params = new URLSearchParams();
    params.set("ch", String(id));
    if (query) params.set("q", query);
    if (order !== DEFAULT_SORT) params.set("order", order);
    if (kind) params.set("kind", kind);
    return `/videos/${m.id}?${params}`;
  };

  return (
    <div className="space-y-5">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(draft.trim());
        }}
        className="card flex flex-wrap items-center gap-3 p-4"
      >
        <div className="relative min-w-[220px] flex-1">
          <SearchIcon className="pointer-events-none absolute left-3.5 top-1/2 size-5 -translate-y-1/2 text-gray-400" />
          <input
            className="field pl-11"
            placeholder="搜索 file_name 或正文 (text) — 留空回到全部"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
          />
        </div>
        <button className="btn btn-primary">搜索</button>
        {(draft || query) && (
          <button
            type="button"
            onClick={() => {
              setDraft("");
              setQuery("");
            }}
            className="btn btn-outline"
          >
            清空
          </button>
        )}
        <SortSelect value={order} onChange={setOrder} className="field field-select w-auto" />
        <KindTabs
          value={kind}
          onChange={setKind}
          counts={{ videos: totalVideos, photos: totalPhotos }}
        />
      </form>

      <div className="text-theme-xs text-gray-500 dark:text-gray-400">
        {query ? (
          <>
            “{query}” 命中{" "}
            <span className="font-medium text-gray-700 dark:text-gray-300">{items.length}</span> 条
            {q.hasNextPage ? " (还有更多)" : ""}
          </>
        ) : (
          <>
            已加载{" "}
            <span className="font-medium text-gray-700 dark:text-gray-300">{items.length}</span> 条
          </>
        )}
      </div>

      {!q.isLoading && items.length === 0 && !query && neverSynced ? (
        <EmptyState
          title="这里还没有内容"
          hint="该话题/频道还没有同步过。点下面的按钮从 Telegram 拉取消息历史,视频和图片都会入库(增量 + 回填,中断可续传)。"
          action={
            <button
              onClick={() => sync.mutate()}
              disabled={sync.isPending}
              className="btn btn-primary"
            >
              <RefreshIcon className="size-4" />
              {sync.isPending ? "启动中…" : "同步这个话题"}
            </button>
          }
        />
      ) : (
        <MediaBrowser
          items={items}
          isLoading={q.isLoading}
          linkTo={linkTo}
          emptyLabel={query ? "无匹配结果" : "暂无内容"}
        />
      )}

      <MoreFooter
        hasNextPage={!!q.hasNextPage}
        isFetchingNextPage={q.isFetchingNextPage}
        fetchNextPage={q.fetchNextPage}
        doneLabel={query ? "已加载全部命中" : "已加载全部"}
        loaded={items.length}
        pageSize={MEDIA_PAGE_SIZE}
      />
    </div>
  );
}

// StreamerList is the top level of the grouped view: a grid of streamer cards.
// Grouping is a videos-only filename convention, so images are out of scope here.
function StreamerList({ id, onPick }: { id: string; onPick: (s: string) => void }) {
  const [filter, setFilter] = useState("");
  const q = useQuery<StreamersResp>({
    queryKey: ["channel", id, "streamers"],
    queryFn: () => api.get(`/api/channels/${id}/streamers`),
  });
  const list = q.data?.streamers ?? [];
  const visible = filter
    ? list.filter((s) => (s.streamer || "其它").toLowerCase().includes(filter.toLowerCase()))
    : list;

  if (q.isLoading) return <LoadingState />;
  if (list.length === 0) return <EmptyState title="暂无视频" />;

  return (
    <div className="space-y-5">
      <div className="card flex flex-wrap items-center gap-3 p-4">
        <div className="relative min-w-[220px] flex-1">
          <SearchIcon className="pointer-events-none absolute left-3.5 top-1/2 size-5 -translate-y-1/2 text-gray-400" />
          <input
            className="field pl-11"
            placeholder="过滤主播…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </div>
        <span className="badge badge-gray">{list.length} 位主播</span>
      </div>

      {visible.length === 0 ? (
        <EmptyState title="没有匹配的主播" />
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6">
          {visible.map((s) => (
            <button
              key={s.streamer || "__other__"}
              onClick={() => onPick(s.streamer)}
              className="card group p-4 text-left transition-colors hover:border-brand-300 hover:bg-brand-25 dark:hover:border-brand-500/40 dark:hover:bg-brand-500/[0.06]"
            >
              <div className="truncate font-medium text-gray-800 transition-colors group-hover:text-brand-600 dark:text-white/90 dark:group-hover:text-brand-400">
                {s.streamer || <span className="text-gray-400">其它 (未匹配)</span>}
              </div>
              <div className="mt-1 text-theme-xs text-gray-500 dark:text-gray-400">
                {s.count.toLocaleString()} 视频
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// StreamerVideos is the drill-down: one streamer's videos, paginated.
function StreamerVideos({ id, streamer, onBack }: { id: string; streamer: string; onBack: () => void }) {
  const [order, setOrder] = useState<SortValue>(DEFAULT_SORT);

  const { query: q, items } = useMediaPages(
    ["channel", id, "streamer-media", streamer, order],
    () => {
      const qs = new URLSearchParams({ streamer });
      if (order !== DEFAULT_SORT) qs.set("order", order);
      return qs;
    },
    { path: `/api/channels/${id}/media` },
  );

  return (
    <div className="space-y-5">
      <div className="card flex flex-wrap items-center gap-3 p-4">
        <button onClick={onBack} className="btn btn-outline btn-sm">
          <ChevronLeftIcon className="size-4" />
          返回主播列表
        </button>
        <span className="truncate font-medium text-gray-800 dark:text-white/90">
          {streamer || "其它 (未匹配)"}
        </span>
        <span className="text-theme-xs text-gray-500 dark:text-gray-400">已加载 {items.length}</span>
        <SortSelect value={order} onChange={setOrder} className="field field-select ml-auto w-auto" />
      </div>

      <MediaBrowser
        items={items}
        isLoading={q.isLoading}
        emptyLabel="该主播暂无视频"
        linkTo={(m) => {
          const params = new URLSearchParams();
          params.set("ch", String(id));
          params.set("streamer", streamer);
          if (order !== DEFAULT_SORT) params.set("order", order);
          return `/videos/${m.id}?${params}`;
        }}
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
