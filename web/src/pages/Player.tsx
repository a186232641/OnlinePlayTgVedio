import { useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, Video } from "../api/client";

interface VideoResp { video: Video; favorite: boolean }

export function Player() {
  const { id } = useParams();
  const qc = useQueryClient();
  const q = useQuery<VideoResp>({
    queryKey: ["video", id],
    queryFn: () => api.get(`/api/videos/${id}`),
    enabled: !!id,
  });

  const fav = useMutation({
    mutationFn: async () => {
      if (!q.data) return;
      if (q.data.favorite) {
        await api.del(`/api/favorites/${q.data.video.id}`);
      } else {
        await api.post(`/api/favorites/`, { video_id: q.data.video.id });
      }
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["video", id] }),
  });

  if (q.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;
  if (!q.data) return <div className="p-6 text-red-400">未找到视频</div>;
  const v = q.data.video;

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-4">
      <video controls preload="metadata" className="w-full bg-black rounded"
             src={v.stream_url} />
      <div className="flex items-start gap-4">
        <div className="flex-1 whitespace-pre-wrap break-words">
          {v.caption || <span className="text-slate-500">无说明</span>}
        </div>
        <button onClick={() => fav.mutate()}
                className={"px-3 py-2 rounded " + (q.data.favorite ? "bg-amber-600" : "bg-slate-800 hover:bg-slate-700")}>
          {q.data.favorite ? "★ 已收藏" : "☆ 收藏"}
        </button>
      </div>
      <div className="text-xs text-slate-500">
        {v.width}×{v.height} · {Math.round(v.size_bytes / 1024 / 1024)} MB · {v.mime}
      </div>
    </div>
  );
}
