import { useQuery } from "@tanstack/react-query";

import { api, ApiError, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";

export function Favorites() {
  const q = useQuery<{ videos: Video[] }>({
    queryKey: ["favorites"],
    queryFn: () => api.get("/api/favorites/"),
  });
  if (q.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;
  if (q.error) {
    const err = q.error as ApiError;
    return (
      <div className="p-6 space-y-2">
        <div className="text-red-400">加载收藏失败</div>
        <pre className="text-xs text-slate-400 whitespace-pre-wrap">
          {`status: ${err.status}\ncode: ${err.code}\nmessage: ${err.message}`}
        </pre>
      </div>
    );
  }
  return (
    <VideoGrid
      videos={q.data?.videos ?? []}
      linkTo={(v) => `/videos/${v.id}?fav=1`}
    />
  );
}
