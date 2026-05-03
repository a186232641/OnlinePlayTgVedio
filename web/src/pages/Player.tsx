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
    <div className="flex flex-col">
      {/* 全宽黑色播放区,视频 max-w-full + max-h-85vh 自动按比例缩放,
          竖屏视频左右加黑边,横屏视频上下加黑边。 */}
      <div className="bg-black w-full flex items-center justify-center min-h-[40vh]">
        <video
          key={v.id}
          controls
          autoPlay
          preload="metadata"
          className="max-w-full max-h-[85vh] outline-none"
          src={v.stream_url}
        />
      </div>

      <div className="px-6 py-4 max-w-5xl mx-auto w-full space-y-3">
        <div className="flex items-start gap-4">
          <div className="flex-1 space-y-1 min-w-0">
            {v.file_name && (
              <div className="font-medium break-all">{v.file_name}</div>
            )}
            <div className="whitespace-pre-wrap break-words text-sm text-slate-300">
              {v.text?.trim() || <span className="text-slate-500">无说明</span>}
            </div>
          </div>
          <button onClick={() => fav.mutate()}
                  className={"px-3 py-2 rounded shrink-0 " + (q.data.favorite ? "bg-amber-600" : "bg-slate-800 hover:bg-slate-700")}>
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
    </div>
  );
}
