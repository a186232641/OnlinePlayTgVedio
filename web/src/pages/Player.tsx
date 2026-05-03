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
        <div className="flex-1 space-y-1">
          {v.file_name && (
            <div className="font-medium break-all">{v.file_name}</div>
          )}
          <div className="whitespace-pre-wrap break-words text-sm text-slate-300">
            {v.text?.trim() || <span className="text-slate-500">无说明</span>}
          </div>
        </div>
        <button onClick={() => fav.mutate()}
                className={"px-3 py-2 rounded " + (q.data.favorite ? "bg-amber-600" : "bg-slate-800 hover:bg-slate-700")}>
          {q.data.favorite ? "★ 已收藏" : "☆ 收藏"}
        </button>
      </div>
      <div className="text-xs text-slate-500 flex flex-wrap gap-3">
        {v.width > 0 && <span>{v.width}×{v.height}</span>}
        {v.file_size > 0 && <span>{(v.file_size / 1024 / 1024).toFixed(1)} MB</span>}
        {v.mime_type && <span>{v.mime_type}</span>}
        {v.date && <span>{v.date.slice(0, 19).replace("T", " ")}</span>}
        {v.from && <span>from: {v.from}</span>}
      </div>
    </div>
  );
}
