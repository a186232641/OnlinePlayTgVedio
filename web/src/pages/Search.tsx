import { useMemo, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";

import { api, Channel, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";

interface Page { videos: Video[] }

const PAGE_SIZE = 200;

export function Search() {
  const [q, setQ] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [channelID, setChannelID] = useState<number>(0);

  const channels = useQuery<{ channels: Channel[] }>({
    queryKey: ["channels", "all"],
    queryFn: () => api.get("/api/channels/"),
  });

  const result = useInfiniteQuery<Page>({
    queryKey: ["search", submitted, channelID],
    enabled: submitted.length > 0,
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      const cursor = pageParam as number;
      const qs = new URLSearchParams({
        q: submitted,
        limit: String(PAGE_SIZE),
      });
      if (cursor > 0) qs.set("offset_id", String(cursor));
      if (channelID > 0) qs.set("channel_id", String(channelID));
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

  return (
    <div>
      <form
        onSubmit={(e) => { e.preventDefault(); setSubmitted(q.trim()); }}
        className="p-4 border-b border-slate-800 flex gap-2 items-center"
      >
        <input
          className="flex-1 px-3 py-2 bg-slate-800 rounded"
          placeholder="搜索视频说明 (支持中文)…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <select
          className="px-3 py-2 bg-slate-800 rounded text-sm"
          value={channelID}
          onChange={(e) => setChannelID(Number(e.target.value))}
        >
          <option value={0}>全部频道</option>
          {(channels.data?.channels ?? [])
            .filter((c) => c.video_count > 0)
            .map((c) => (
              <option key={c.id} value={c.id}>{c.title} ({c.video_count})</option>
            ))}
        </select>
        <button className="px-4 py-2 bg-emerald-700 hover:bg-emerald-600 rounded">搜索</button>
      </form>

      {!submitted && (
        <div className="p-6 text-slate-400 text-sm">输入关键字后回车,在 caption 里全字符匹配。</div>
      )}

      {submitted && result.isLoading && (
        <div className="p-6 text-slate-400">搜索中…</div>
      )}

      {submitted && result.data && (
        <>
          <div className="px-6 pt-4 text-xs text-slate-500">
            "{submitted}" 命中 {all.length} 条{channelID > 0 ? "(单频道)" : ""}
          </div>
          <VideoGrid videos={all} />
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
