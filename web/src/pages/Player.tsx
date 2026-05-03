import { useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { api, Video } from "../api/client";

interface VideoResp { video: Video; favorite: boolean }

const MEDIA_ERR_LABEL: Record<number, string> = {
  1: "ABORTED 用户取消加载",
  2: "NETWORK 网络层错误(后端 502/503/连接断开)",
  3: "DECODE 解码失败(浏览器不支持该编码,如 H.265)",
  4: "SRC_NOT_SUPPORTED 服务端返回不能识别为视频(可能是 JSON 错误响应)",
};

export function Player() {
  const { id } = useParams();
  const qc = useQueryClient();
  const [mediaErr, setMediaErr] = useState<string | null>(null);
  const [streamDiag, setStreamDiag] = useState<string | null>(null);

  // Reset error state when video changes
  useEffect(() => { setMediaErr(null); setStreamDiag(null); }, [id]);

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
      <div className="bg-black w-full flex items-center justify-center min-h-[40vh] relative">
        <video
          key={v.id}
          controls
          autoPlay
          preload="metadata"
          className="max-w-full max-h-[85vh] outline-none"
          src={v.stream_url}
          onError={async (e) => {
            const code = (e.currentTarget.error?.code ?? 0);
            setMediaErr(MEDIA_ERR_LABEL[code] ?? `未知(code=${code})`);
            // Try to fetch the stream URL directly to surface backend error body.
            try {
              const r = await fetch(v.stream_url, { credentials: "include" });
              if (!r.ok) {
                const body = await r.text();
                setStreamDiag(`HTTP ${r.status} ${r.statusText}\n${body.slice(0, 400)}`);
              }
            } catch (err: any) {
              setStreamDiag(`fetch failed: ${err.message ?? err}`);
            }
          }}
        />
        {mediaErr && (
          <div className="absolute inset-x-0 bottom-0 bg-red-950/90 text-red-200 text-xs p-3 space-y-1">
            <div className="font-medium">播放失败: {mediaErr}</div>
            {streamDiag && <pre className="text-[10px] whitespace-pre-wrap text-red-100/80">{streamDiag}</pre>}
            <div className="text-red-300/60">stream_url: {v.stream_url}</div>
          </div>
        )}
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
