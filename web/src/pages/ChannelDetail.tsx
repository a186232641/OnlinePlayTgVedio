import { useParams } from "react-router-dom";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef } from "react";

import { api, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";

interface Page { videos: Video[]; total?: number }

const PAGE_SIZE = 200;

export function ChannelDetail() {
  const { id } = useParams();
  const sentinelRef = useRef<HTMLDivElement | null>(null);

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
      // Last fetched page returned a full batch → there might be more.
      if (last.videos.length < PAGE_SIZE) return undefined;
      const tail = last.videos[last.videos.length - 1];
      return tail?.id;
    },
  });

  const all = useMemo<Video[]>(
    () => q.data?.pages.flatMap((p) => p.videos) ?? [],
    [q.data],
  );
  const total = q.data?.pages[0]?.total;

  // Auto-load when sentinel comes into view (infinite scroll).
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    const obs = new IntersectionObserver((entries) => {
      if (entries[0].isIntersecting && q.hasNextPage && !q.isFetchingNextPage) {
        q.fetchNextPage();
      }
    }, { rootMargin: "400px" });
    obs.observe(el);
    return () => obs.disconnect();
  }, [q.hasNextPage, q.isFetchingNextPage, q.fetchNextPage]);

  if (q.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;

  return (
    <div className="space-y-2">
      <div className="px-6 pt-4 text-xs text-slate-500">
        {total != null ? <>已加载 {all.length} / {total}</> : <>已加载 {all.length} 条</>}
      </div>
      <VideoGrid videos={all} />
      <div ref={sentinelRef} className="h-12 flex items-center justify-center text-xs text-slate-500">
        {q.isFetchingNextPage
          ? "加载中…"
          : q.hasNextPage
          ? <button onClick={() => q.fetchNextPage()} className="px-4 py-1.5 bg-slate-800 hover:bg-slate-700 rounded">加载更多</button>
          : all.length > 0 ? "已加载全部" : null}
      </div>
    </div>
  );
}
