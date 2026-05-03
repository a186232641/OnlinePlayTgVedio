import { useParams } from "react-router-dom";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { api, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";

interface Page { videos: Video[]; total?: number }

const PAGE_SIZE = 200;

export function ChannelDetail() {
  const { id } = useParams();

  const q = useInfiniteQuery<Page>({
    queryKey: ["channel", id, "videos"],
    enabled: !!id,
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      const cursor = pageParam as number;
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE) });
      if (cursor > 0) qs.set("offset_id", String(cursor));
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

  if (q.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;

  return (
    <div>
      <div className="px-6 pt-4 text-xs text-slate-500">
        {total != null ? <>已加载 {all.length} / {total}</> : <>已加载 {all.length} 条</>}
      </div>
      <VideoGrid videos={all} />
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
          <span className="text-xs text-slate-500">— 已加载全部 {all.length} 条 —</span>
        ) : null}
      </div>
    </div>
  );
}
