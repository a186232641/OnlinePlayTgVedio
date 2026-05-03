import { useQuery } from "@tanstack/react-query";

import { api, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";

export function Favorites() {
  const q = useQuery<{ videos: Video[] }>({
    queryKey: ["favorites"],
    queryFn: () => api.get("/api/favorites/"),
  });
  if (q.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;
  return (
    <VideoGrid
      videos={q.data?.videos ?? []}
      linkTo={(v) => `/videos/${v.id}?fav=1`}
    />
  );
}
