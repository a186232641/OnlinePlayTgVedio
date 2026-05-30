import { useParams } from "react-router-dom";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { api, Channel, Streamer, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";

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
    <div>
      <div className="px-6 pt-4 pb-2 flex items-center gap-3 flex-wrap">
        <h1 className="text-lg font-semibold truncate">{channel?.title ?? "频道"}</h1>
        <span className="text-xs text-slate-500">{channel?.video_count ?? 0} 视频</span>
        <label className="ml-auto flex items-center gap-2 text-sm text-slate-300 select-none">
          <span>按主播分组</span>
          <button
            type="button"
            role="switch"
            aria-checked={grouped}
            disabled={!channel || toggleGroup.isPending}
            onClick={() => toggleGroup.mutate(!grouped)}
            className={
              "relative w-10 h-6 rounded-full transition-colors disabled:opacity-50 " +
              (grouped ? "bg-emerald-600" : "bg-slate-700")
            }
          >
            <span
              className={
                "absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform " +
                (grouped ? "translate-x-4" : "")
              }
            />
          </button>
        </label>
      </div>

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

  const q = useInfiniteQuery<Page>({
    queryKey: ["channel", id, "videos", query],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      const cursor = pageParam as number;
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE) });
      if (cursor > 0) qs.set("offset_id", String(cursor));
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
    <>
      <form
        onSubmit={(e) => { e.preventDefault(); setQuery(draft.trim()); }}
        className="px-6 pb-2 flex gap-2 items-center"
      >
        <input
          className="flex-1 px-3 py-1.5 bg-slate-800 rounded text-sm"
          placeholder="搜索 file_name 或正文 (text) — 留空回到全部"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
        <button className="px-4 py-1.5 bg-emerald-700 hover:bg-emerald-600 rounded text-sm">搜索</button>
        {(draft || query) && (
          <button
            type="button"
            onClick={() => { setDraft(""); setQuery(""); }}
            className="px-3 py-1.5 bg-slate-700 hover:bg-slate-600 rounded text-sm"
          >清空</button>
        )}
      </form>

      <div className="px-6 text-xs text-slate-500">
        {query ? (
          <>"{query}" 命中 {all.length} 条{q.hasNextPage ? " (还有更多)" : ""}</>
        ) : total != null ? (
          <>已加载 {all.length} / {total}</>
        ) : (
          <>已加载 {all.length} 条</>
        )}
      </div>

      {q.isLoading ? (
        <div className="p-6 text-slate-400">加载中…</div>
      ) : (
        <VideoGrid
          videos={all}
          linkTo={(v) => {
            const params = new URLSearchParams();
            params.set("ch", String(id));
            if (query) params.set("q", query);
            return `/videos/${v.id}?${params}`;
          }}
        />
      )}

      <MoreFooter q={q} emptyLabel={query ? "无匹配结果" : "暂无视频"} doneLabel={query ? "已加载全部命中" : "已加载全部"} loaded={all.length} />
    </>
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

  if (q.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;
  if (list.length === 0) return <div className="p-6 text-slate-400">暂无视频</div>;

  return (
    <>
      <div className="px-6 pb-2 flex items-center gap-3">
        <input
          className="flex-1 px-3 py-1.5 bg-slate-800 rounded text-sm"
          placeholder="过滤主播…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        <span className="text-xs text-slate-500">{list.length} 位主播</span>
      </div>
      <div className="px-6 pb-6 grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-3">
        {visible.map((s) => (
          <button
            key={s.streamer || "__other__"}
            onClick={() => onPick(s.streamer)}
            className="p-4 rounded bg-slate-900 border border-slate-800 hover:border-emerald-700 text-left"
          >
            <div className="font-medium truncate">
              {s.streamer || <span className="text-slate-400">其它 (未匹配)</span>}
            </div>
            <div className="text-xs text-slate-500 mt-1">{s.count} 视频</div>
          </button>
        ))}
      </div>
    </>
  );
}

// StreamerVideos is the drill-down: one streamer's videos, paginated.
function StreamerVideos({ id, streamer, onBack }: { id: string; streamer: string; onBack: () => void }) {
  const q = useInfiniteQuery<Page>({
    queryKey: ["channel", id, "streamer-videos", streamer],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      const cursor = pageParam as number;
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE), streamer });
      if (cursor > 0) qs.set("offset_id", String(cursor));
      return api.get<Page>(`/api/channels/${id}/videos?${qs}`);
    },
    getNextPageParam: (last) =>
      last.videos.length < PAGE_SIZE ? undefined : last.videos[last.videos.length - 1]?.id,
  });
  const all = useMemo<Video[]>(() => q.data?.pages.flatMap((p) => p.videos) ?? [], [q.data]);

  return (
    <>
      <div className="px-6 pb-2 flex items-center gap-3">
        <button onClick={onBack} className="text-sm text-emerald-400 hover:text-emerald-300">← 返回主播列表</button>
        <span className="text-sm font-medium truncate">{streamer || "其它 (未匹配)"}</span>
        <span className="text-xs text-slate-500">已加载 {all.length}</span>
      </div>

      {q.isLoading ? (
        <div className="p-6 text-slate-400">加载中…</div>
      ) : (
        <VideoGrid
          videos={all}
          linkTo={(v) => {
            const params = new URLSearchParams();
            params.set("ch", String(id));
            params.set("streamer", streamer);
            return `/videos/${v.id}?${params}`;
          }}
        />
      )}

      <MoreFooter q={q} emptyLabel="该主播暂无视频" doneLabel="已加载全部" loaded={all.length} />
    </>
  );
}

// MoreFooter renders the shared "load more / done / empty" footer for an
// infinite query.
function MoreFooter({
  q, emptyLabel, doneLabel, loaded,
}: {
  q: ReturnType<typeof useInfiniteQuery<Page>>;
  emptyLabel: string;
  doneLabel: string;
  loaded: number;
}) {
  return (
    <div className="py-6 flex items-center justify-center">
      {q.hasNextPage ? (
        <button
          onClick={() => q.fetchNextPage()}
          disabled={q.isFetchingNextPage}
          className="px-6 py-2 bg-emerald-700 hover:bg-emerald-600 disabled:opacity-50 rounded text-sm"
        >
          {q.isFetchingNextPage ? "加载中…" : `加载下一页 (+${PAGE_SIZE})`}
        </button>
      ) : loaded > 0 ? (
        <span className="text-xs text-slate-500">— {doneLabel} {loaded} 条 —</span>
      ) : !q.isLoading ? (
        <span className="text-xs text-slate-500">{emptyLabel}</span>
      ) : null}
    </div>
  );
}
