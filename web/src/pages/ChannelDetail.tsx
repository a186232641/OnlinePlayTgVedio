import { useParams } from "react-router-dom";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { api, Channel, Streamer, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";
import { SortSelect, SortValue, DEFAULT_SORT } from "../components/SortSelect";
import { ChevronLeftIcon, SearchIcon } from "../components/icons";
import { EmptyState, LoadingState, MoreFooter, PageHeader, Toggle } from "../components/ui";

interface Page { videos: Video[]; total?: number }
interface ChannelResp { channel: Channel }
interface StreamersResp { streamers: Streamer[] }

const PAGE_SIZE = 200;

export function ChannelDetail() {
  const { id } = useParams();
  const qc = useQueryClient();

  const channelQ = useQuery<ChannelResp>({
    queryKey: ["channel", id],
    queryFn: () => api.get(`/api/channels/${id}`),
    enabled: !!id,
  });
  const channel = channelQ.data?.channel;
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

  return (
    <div className="space-y-5 p-4 md:p-6">
      <PageHeader
        title={channel?.title ?? "频道"}
        meta={
          <>
            {(channel?.video_count ?? 0).toLocaleString()} 个视频
            {channel?.username && ` · @${channel.username}`}
          </>
        }
        actions={
          <Toggle
            label="按主播分组"
            title="按文件名里的主播名把视频分组展示"
            checked={grouped}
            disabled={!channel || toggleGroup.isPending}
            onChange={(v) => toggleGroup.mutate(v)}
          />
        }
      />

      {!grouped && <UngroupedView id={id!} />}
      {grouped && selected === null && <StreamerList id={id!} onPick={setSelected} />}
      {grouped && selected !== null && (
        <StreamerVideos id={id!} streamer={selected} onBack={() => setSelected(null)} />
      )}
    </div>
  );
}

// UngroupedView is the classic "all videos + free search" channel view.
function UngroupedView({ id }: { id: string }) {
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const [order, setOrder] = useState<SortValue>(DEFAULT_SORT);

  const q = useInfiniteQuery<Page>({
    queryKey: ["channel", id, "videos", query, order],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      const cursor = pageParam as number;
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE) });
      if (cursor > 0) qs.set("offset_id", String(cursor));
      if (order !== DEFAULT_SORT) qs.set("order", order);
      if (query) {
        qs.set("q", query);
        qs.set("channel_id", String(id));
        return api.get<Page>(`/api/videos/search?${qs}`);
      }
      return api.get<Page>(`/api/channels/${id}/videos?${qs}`);
    },
    getNextPageParam: (last) =>
      last.videos.length < PAGE_SIZE ? undefined : last.videos[last.videos.length - 1]?.id,
  });

  const all = useMemo<Video[]>(() => q.data?.pages.flatMap((p) => p.videos) ?? [], [q.data]);
  const total = q.data?.pages[0]?.total;

  return (
    <div className="space-y-5">
      <form
        onSubmit={(e) => { e.preventDefault(); setQuery(draft.trim()); }}
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
            onClick={() => { setDraft(""); setQuery(""); }}
            className="btn btn-outline"
          >清空</button>
        )}
        <SortSelect value={order} onChange={setOrder} className="field field-select w-auto" />
      </form>

      <div className="text-theme-xs text-gray-500 dark:text-gray-400">
        {query ? (
          <>“{query}” 命中 <span className="font-medium text-gray-700 dark:text-gray-300">{all.length}</span> 条{q.hasNextPage ? " (还有更多)" : ""}</>
        ) : total != null ? (
          <>已加载 <span className="font-medium text-gray-700 dark:text-gray-300">{all.length}</span> / {total.toLocaleString()}</>
        ) : (
          <>已加载 {all.length} 条</>
        )}
      </div>

      {q.isLoading ? (
        <LoadingState />
      ) : (
        <VideoGrid
          videos={all}
          emptyLabel={query ? "无匹配结果" : "暂无视频"}
          linkTo={(v) => {
            const params = new URLSearchParams();
            params.set("ch", String(id));
            if (query) params.set("q", query);
            if (order !== DEFAULT_SORT) params.set("order", order);
            return `/videos/${v.id}?${params}`;
          }}
        />
      )}

      <MoreFooter
        hasNextPage={!!q.hasNextPage}
        isFetchingNextPage={q.isFetchingNextPage}
        fetchNextPage={q.fetchNextPage}
        doneLabel={query ? "已加载全部命中" : "已加载全部"}
        loaded={all.length}
        pageSize={PAGE_SIZE}
      />
    </div>
  );
}

// StreamerList is the top level of the grouped view: a grid of streamer cards.
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
  const q = useInfiniteQuery<Page>({
    queryKey: ["channel", id, "streamer-videos", streamer, order],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      const cursor = pageParam as number;
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE), streamer });
      if (cursor > 0) qs.set("offset_id", String(cursor));
      if (order !== DEFAULT_SORT) qs.set("order", order);
      return api.get<Page>(`/api/channels/${id}/videos?${qs}`);
    },
    getNextPageParam: (last) =>
      last.videos.length < PAGE_SIZE ? undefined : last.videos[last.videos.length - 1]?.id,
  });
  const all = useMemo<Video[]>(() => q.data?.pages.flatMap((p) => p.videos) ?? [], [q.data]);

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
        <span className="text-theme-xs text-gray-500 dark:text-gray-400">已加载 {all.length}</span>
        <SortSelect value={order} onChange={setOrder} className="field field-select ml-auto w-auto" />
      </div>

      {q.isLoading ? (
        <LoadingState />
      ) : (
        <VideoGrid
          videos={all}
          emptyLabel="该主播暂无视频"
          linkTo={(v) => {
            const params = new URLSearchParams();
            params.set("ch", String(id));
            params.set("streamer", streamer);
            if (order !== DEFAULT_SORT) params.set("order", order);
            return `/videos/${v.id}?${params}`;
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
