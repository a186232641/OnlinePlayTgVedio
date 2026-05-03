import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";

export function ChannelDetail() {
  const { id } = useParams();
  const q = useQuery<{ videos: Video[] }>({
    queryKey: ["channel", id, "videos"],
    queryFn: () => api.get(`/api/channels/${id}/videos`),
    enabled: !!id,
  });
  if (q.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;
  return <VideoGrid videos={q.data?.videos ?? []} />;
}
