import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, Channel, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";

// ChannelDetail dispatches by dialog_kind:
//   forum  → list its child topics (no own message stream)
//   else   → list videos from DB + a "load more from Telegram" button that
//            live-fetches (and lazily upserts) the next page.
export function ChannelDetail() {
  const { id } = useParams();
  const qc = useQueryClient();

  const channels = useQuery<{ channels: Channel[] }>({
    queryKey: ["channels", "all"],
    queryFn: () => api.get("/api/channels/"),
  });
  const me = channels.data?.channels.find((c) => String(c.id) === id);

  const videos = useQuery<{ videos: Video[] }>({
    queryKey: ["channel", id, "videos"],
    queryFn: () => api.get(`/api/channels/${id}/videos`),
    enabled: !!id && me?.dialog_kind !== "forum",
  });

  const liveFetch = useMutation({
    mutationFn: async (offsetMsgId?: number) => {
      const qs = offsetMsgId ? `?offset_msg_id=${offsetMsgId}` : "";
      return api.get<{ videos: Video[]; fetched: number }>(`/api/channels/${id}/live-videos${qs}`);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["channel", id, "videos"] }),
  });

  if (channels.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;
  if (!me) return <div className="p-6 text-red-400">频道不存在</div>;

  if (me.dialog_kind === "forum") {
    return <ForumTopics forum={me} all={channels.data?.channels ?? []} />;
  }

  const list = videos.data?.videos ?? [];
  const oldest = list.length > 0 ? list[list.length - 1] : null;

  return (
    <div className="space-y-2">
      <div className="px-6 pt-4 flex items-center gap-3">
        <h1 className="text-xl font-semibold">{me.title}</h1>
        <span className="text-xs text-slate-500">{list.length} 条已加载</span>
        <div className="flex-1" />
        <button
          onClick={() => liveFetch.mutate(undefined)}
          disabled={liveFetch.isPending}
          className="px-3 py-1.5 text-xs bg-sky-800 hover:bg-sky-700 disabled:opacity-50 rounded"
          title="向 Telegram 现取最新一页视频"
        >{liveFetch.isPending ? "加载中…" : "刷新最新"}</button>
        {oldest && (
          <button
            onClick={() => liveFetch.mutate(Number(oldest.id))}
            disabled={liveFetch.isPending}
            className="px-3 py-1.5 text-xs bg-slate-700 hover:bg-slate-600 disabled:opacity-50 rounded"
            title="向 Telegram 取更早的一页"
          >加载更早</button>
        )}
      </div>
      {list.length === 0 && !liveFetch.isPending && (
        <div className="px-6 text-slate-400">
          DB 暂无缓存视频。点上方"刷新最新"从 Telegram 实时拉取。
        </div>
      )}
      <VideoGrid videos={list} />
    </div>
  );
}

function ForumTopics({ forum, all }: { forum: Channel; all: Channel[] }) {
  const topics = all.filter((c) => c.parent_channel_id === forum.id);
  return (
    <div className="p-6 space-y-3">
      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">{forum.title}</h1>
        <span className="text-xs text-slate-500">{topics.length} 个话题</span>
      </div>
      {topics.length === 0 && (
        <div className="text-slate-400">这个论坛暂未发现话题,试试在 TG 账号管理页"重新发现"。</div>
      )}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
        {topics.map((t) => (
          <Link
            key={t.id}
            to={`/channels/${t.id}`}
            className="p-4 rounded bg-slate-900 border border-slate-800 hover:border-emerald-700"
          >
            <div className="font-medium truncate">{t.title}</div>
            <div className="text-xs text-slate-500 mt-2 flex items-center gap-2">
              <span>{t.video_count} 视频已索引</span>
              {t.index_enabled && <span className="text-emerald-400">索引开启</span>}
              {t.index_status === "running" && <span className="text-amber-300">索引中</span>}
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}
