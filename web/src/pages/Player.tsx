import { useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import mpegts from "mpegts.js";

import { api, Video } from "../api/client";

interface VideoResp { video: Video; favorite: boolean }

const MEDIA_ERR_LABEL: Record<number, string> = {
  1: "ABORTED 用户取消加载",
  2: "NETWORK 网络层错误(后端 502/503/连接断开)",
  3: "DECODE 解码失败(浏览器不支持该编码,如 H.265)",
  4: "SRC_NOT_SUPPORTED 服务端返回不能识别为视频",
};

export function Player() {
  const { id } = useParams();
  const qc = useQueryClient();
  const [mediaErr, setMediaErr] = useState<string | null>(null);
  const [streamDiag, setStreamDiag] = useState<string | null>(null);
  const [containerHint, setContainerHint] = useState<string>("");
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const flvPlayerRef = useRef<mpegts.Player | null>(null);

  useEffect(() => {
    setMediaErr(null);
    setStreamDiag(null);
    setContainerHint("");
  }, [id]);

  const q = useQuery<VideoResp>({
    queryKey: ["video", id],
    queryFn: () => api.get(`/api/videos/${id}`),
    enabled: !!id,
  });

  // Set up the player based on container detection. Many TG-stored videos
  // have mime_type=video/mp4 in the JSON but the actual bytes are FLV
  // (browser can't decode natively). Probe first 16 bytes, then either
  // attach the URL directly to <video> (for real mp4) or hand it to mpegts.js.
  useEffect(() => {
    const video = videoRef.current;
    const url = q.data?.video.stream_url;
    if (!video || !url) return;

    let cancelled = false;
    setMediaErr(null);
    setStreamDiag(null);

    (async () => {
      let kind: "flv" | "native" = "native";
      let hint = "";
      try {
        const r = await fetch(url, {
          headers: { Range: "bytes=0-15" },
          credentials: "include",
        });
        if (r.ok) {
          const ab = await r.arrayBuffer();
          const b = new Uint8Array(ab);
          if (b.length >= 3 && b[0] === 0x46 && b[1] === 0x4c && b[2] === 0x56) {
            kind = "flv";
            hint = "FLV container — using mpegts.js";
          } else if (b.length >= 8 && b[4] === 0x66 && b[5] === 0x74 && b[6] === 0x79 && b[7] === 0x70) {
            hint = "MP4 (ftyp box) — native";
          } else {
            hint = "未识别的容器,尝试 native";
          }
        }
      } catch {
        // probe failed, fall back to native
      }
      if (cancelled) return;
      setContainerHint(hint);

      if (kind === "flv") {
        if (!mpegts.getFeatureList().mseLivePlayback) {
          setMediaErr("浏览器不支持 MSE — FLV 视频无法播放");
          return;
        }
        const player = mpegts.createPlayer({
          type: "flv",
          url,
          isLive: false,
          cors: true,
          withCredentials: true,
        });
        player.attachMediaElement(video);
        player.on(mpegts.Events.ERROR, (errType, errDetail, errInfo) => {
          setMediaErr(`mpegts ${errType}: ${errDetail}`);
          setStreamDiag(JSON.stringify(errInfo));
        });
        player.load();
        flvPlayerRef.current = player;
        try {
          // Some browsers block autoplay; ignore rejection.
          await player.play();
        } catch {
          /* user can press play manually */
        }
      } else {
        video.src = url;
        try {
          await video.play();
        } catch {
          /* autoplay blocked, no-op */
        }
      }
    })();

    return () => {
      cancelled = true;
      if (flvPlayerRef.current) {
        try { flvPlayerRef.current.destroy(); } catch { /* noop */ }
        flvPlayerRef.current = null;
      }
      try {
        video.pause();
        video.removeAttribute("src");
        video.load();
      } catch { /* noop */ }
    };
  }, [q.data?.video.stream_url]);

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
      <div className="bg-black w-full flex items-center justify-center min-h-[40vh] relative">
        <video
          key={v.id}
          ref={videoRef}
          controls
          preload="metadata"
          className="max-w-full max-h-[85vh] outline-none"
          onError={async (e) => {
            const code = (e.currentTarget.error?.code ?? 0);
            setMediaErr(MEDIA_ERR_LABEL[code] ?? `未知(code=${code})`);
            try {
              const r = await fetch(v.stream_url, { credentials: "include" });
              const ct = r.headers.get("content-type") ?? "(none)";
              const cl = r.headers.get("content-length") ?? "(none)";
              const cr = r.headers.get("content-range") ?? "(none)";
              const ab = await r.arrayBuffer();
              const head = Array.from(new Uint8Array(ab.slice(0, 16)))
                .map((b) => b.toString(16).padStart(2, "0"))
                .join(" ");
              setStreamDiag(
                `HTTP ${r.status} ${r.statusText}\n` +
                `Content-Type: ${ct}\nContent-Length: ${cl}\nContent-Range: ${cr}\n` +
                `Body bytes: ${ab.byteLength}\nFirst 16 bytes (hex): ${head}`,
              );
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
          {containerHint && <span className="text-emerald-500">{containerHint}</span>}
        </div>
      </div>
    </div>
  );
}
