import { useParams } from "react-router-dom";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { api, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";

interface Page { videos: Video[]; total?: number }

const PAGE_SIZE = 200;

export function ChannelDetail() {
  const { id } = useParams();
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");

  // List mode (default) and search mode share the same useInfiniteQuery
  // shape; we just swap the URL based on whether `query` is empty.
  const q = useInfiniteQuery<Page>({
    queryKey: ["channel", id, "videos", query],
    enabled: !!id,
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
    getNextPageParam: (last) => {
      if (last.videos.length < PAGE_SIZE) return undefined;
      return last.videos[last.videos.length - 1]?.id;
    },
  });

  const all = useMemo<Video[]>(
    () => q.data?.pages.flatMap((p) => p.videos) ?? [],
    [q.data],
  );
  const total = q.data?.pages[0]?.total;

  return (
    <div>
      <form
        onSubmit={(e) => { e.preventDefault(); setQuery(draft.trim()); }}
        className="px-6 pt-4 pb-2 flex gap-2 items-center"
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
        <VideoGrid videos={all} />
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
          <span className="text-xs text-slate-500">— {query ? "已加载全部命中" : "已加载全部"} {all.length} 条 —</span>
        ) : !q.isLoading ? (
          <span className="text-xs text-slate-500">{query ? "无匹配结果" : "暂无视频"}</span>
        ) : null}
      </div>
    </div>
  );
}
