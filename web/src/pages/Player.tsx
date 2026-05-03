import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import mpegts from "mpegts.js";

import { api, Video } from "../api/client";

interface VideoResp { video: Video; favorite: boolean }
interface VideosResp { videos: Video[] }

const MEDIA_ERR_LABEL: Record<number, string> = {
  1: "ABORTED 用户取消加载",
  2: "NETWORK 网络层错误(后端 502/503/连接断开)",
  3: "DECODE 解码失败(浏览器不支持该编码,如 H.265)",
  4: "SRC_NOT_SUPPORTED 服务端返回不能识别为视频",
};

const PLAYLIST_LIMIT = 500;

// playlistRequest builds the upstream URL based on URL params:
//   ?ch=13                  → channel videos
//   ?q=foo&ch=13            → search results (optionally channel-scoped)
//   ?text=...&date_from=... → advanced search filters
//   ?fav=1                  → favorites
function playlistRequest(p: URLSearchParams): string | null {
  const ch = p.get("ch");
  const q = p.get("q");
  const text = p.get("text");
  const fileName = p.get("file_name");
  const dateFrom = p.get("date_from");
  const dateTo = p.get("date_to");
  const fav = p.get("fav");

  if (fav) return "/api/favorites/";

  const hasSearch = !!(q || text || fileName || dateFrom || dateTo);
  if (hasSearch) {
    const qs = new URLSearchParams({ limit: String(PLAYLIST_LIMIT) });
    if (q) qs.set("q", q);
    if (text) qs.set("text", text);
    if (fileName) qs.set("file_name", fileName);
    if (dateFrom) qs.set("date_from", dateFrom);
    if (dateTo) qs.set("date_to", dateTo);
    if (ch) qs.set("channel_id", ch);
    return `/api/videos/search?${qs}`;
  }
  if (ch) {
    return `/api/channels/${ch}/videos?limit=${PLAYLIST_LIMIT}`;
  }
  return null;
}

export function Player() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const qc = useQueryClient();
  const [mediaErr, setMediaErr] = useState<string | null>(null);
  const [streamDiag, setStreamDiag] = useState<string | null>(null);
  const [containerHint, setContainerHint] = useState<string>("");
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const flvPlayerRef = useRef<mpegts.Player | null>(null);
  const sidebarRef = useRef<HTMLElement | null>(null);

  // Stable string of the search params; used both as react-query cache key
  // and as a dependency for the "scroll-into-view" effect.
  const playlistKey = searchParams.toString();

  useEffect(() => {
    setMediaErr(null);
    setStreamDiag(null);
    setContainerHint("");
  }, [id]);

  // Position the highlighted item inside the sidebar — but only by setting
  // aside.scrollTop directly, never via scrollIntoView (which can cascade up
  // to ancestor scroll containers and yank the whole page to the top).
  // Strategy: if the current item's bounding box is already inside the
  // aside's visible window, do nothing. Otherwise center it.
  useEffect(() => {
    const aside = sidebarRef.current;
    if (!aside || !id) return;
    const el = aside.querySelector<HTMLElement>(`[data-video-id="${id}"]`);
    if (!el) return;
    const top = el.offsetTop - aside.offsetTop;
    const bottom = top + el.offsetHeight;
    const viewTop = aside.scrollTop;
    const viewBottom = viewTop + aside.clientHeight;
    if (top >= viewTop && bottom <= viewBottom) {
      return; // already visible — leave the user's scroll position alone
    }
    aside.scrollTop = Math.max(0, top - aside.clientHeight / 2 + el.offsetHeight / 2);
  }, [id, playlistKey]);

  const meta = useQuery<VideoResp>({
    queryKey: ["video", id],
    queryFn: () => api.get(`/api/videos/${id}`),
    enabled: !!id,
    // Keep showing the previous video's metadata while fetching the next, so
    // the layout (especially the playlist sidebar) doesn't unmount/remount
    // and lose its scroll position.
    placeholderData: keepPreviousData,
  });

  // Playlist (siblings from the same context). queryKey only depends on
  // search-params content,so it doesn't refetch when only the video id changes.
  const playlist = useQuery<VideosResp>({
    queryKey: ["playlist", playlistKey],
    enabled: !!playlistRequest(searchParams),
    queryFn: () => {
      const url = playlistRequest(searchParams);
      if (!url) return Promise.resolve({ videos: [] });
      return api.get<VideosResp>(url);
    },
  });

  const list = playlist.data?.videos ?? [];
  const currentIdx = useMemo(
    () => list.findIndex((v) => String(v.id) === id),
    [list, id],
  );
  const next = currentIdx >= 0 && currentIdx < list.length - 1 ? list[currentIdx + 1] : null;
  const prev = currentIdx > 0 ? list[currentIdx - 1] : null;

  // Container detection + player setup
  useEffect(() => {
    const video = videoRef.current;
    const url = meta.data?.video.stream_url;
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
            hint = "FLV — mpegts.js";
          } else if (b.length >= 8 && b[4] === 0x66 && b[5] === 0x74 && b[6] === 0x79 && b[7] === 0x70) {
            hint = "MP4 (ftyp)";
          } else {
            hint = "未识别容器,试试 native";
          }
        }
      } catch {
        // probe failed
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
        try { await player.play(); } catch { /* autoplay block */ }
      } else {
        video.src = url;
        try { await video.play(); } catch { /* autoplay block */ }
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
  }, [meta.data?.video.stream_url]);

  const fav = useMutation({
    mutationFn: async () => {
      if (!meta.data) return;
      if (meta.data.favorite) {
        await api.del(`/api/favorites/${meta.data.video.id}`);
      } else {
        await api.post(`/api/favorites/`, { video_id: meta.data.video.id });
      }
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["video", id] }),
  });

  const goToVideo = (vid: number) => {
    const params = searchParams.toString();
    navigate(`/videos/${vid}${params ? "?" + params : ""}`);
  };

  const v = meta.data?.video;
  const hasPlaylist = list.length > 0;
  const showLoading = meta.isLoading && !meta.data;
  const showNotFound = !meta.isLoading && !meta.data;

  return (
    <div className="flex flex-col">
      <div className="flex flex-col lg:flex-row">
        {/* video area */}
        <div className="lg:flex-1 bg-black flex items-center justify-center min-h-[40vh] relative">
          {showLoading && <div className="text-slate-400">加载中…</div>}
          {showNotFound && <div className="text-red-400 p-6">未找到视频</div>}
          {v && (
            <>
              <video
                key={v.id}
                ref={videoRef}
                controls
                preload="metadata"
                className="max-w-full max-h-[85vh] outline-none"
                onEnded={() => {
                  if (next) goToVideo(next.id);
                }}
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
            </>
          )}
        </div>

        {/* playlist sidebar */}
        {hasPlaylist && (
          <aside ref={sidebarRef} className="lg:w-80 max-h-[85vh] overflow-y-auto bg-slate-900 border-l border-slate-800 shrink-0">
            <div className="px-3 py-2 text-xs text-slate-400 border-b border-slate-800 sticky top-0 bg-slate-900">
              播放列表 · {list.length} 条 ({currentIdx + 1}/{list.length})
            </div>
            {list.map((item, i) => {
              const isCurrent = String(item.id) === id;
              return (
                <button
                  key={item.id}
                  data-video-id={item.id}
                  onClick={() => goToVideo(item.id)}
                  className={
                    "w-full text-left px-3 py-2 border-b border-slate-800/50 flex gap-2 items-start " +
                    (isCurrent ? "bg-emerald-900/40" : "hover:bg-slate-800")
                  }
                >
                  <span className={"text-xs w-6 shrink-0 " + (isCurrent ? "text-emerald-300" : "text-slate-500")}>
                    {isCurrent ? "▶" : i + 1}
                  </span>
                  <div className="flex-1 min-w-0">
                    <div className={"text-xs leading-snug line-clamp-2 break-all " + (isCurrent ? "text-emerald-200" : "")}>
                      {item.file_name?.trim() || item.text?.trim() || `视频 #${item.id}`}
                    </div>
                    <div className="text-[10px] text-slate-500 mt-0.5">
                      {item.duration_seconds > 0 && fmtDur(item.duration_seconds)}
                      {item.date && " · " + item.date.slice(0, 10)}
                    </div>
                  </div>
                </button>
              );
            })}
          </aside>
        )}
      </div>

      {/* metadata + controls */}
      {v && meta.data && (
        <div className="px-6 py-4 max-w-5xl mx-auto w-full space-y-3">
          <div className="flex items-start gap-4">
            <div className="flex-1 space-y-1 min-w-0">
              {v.file_name && <div className="font-medium break-all">{v.file_name}</div>}
              <div className="whitespace-pre-wrap break-words text-sm text-slate-300">
                {v.text?.trim() || <span className="text-slate-500">无说明</span>}
              </div>
            </div>
            <div className="flex flex-col gap-2 shrink-0">
              <button onClick={() => fav.mutate()}
                      className={"px-3 py-2 rounded text-sm " + (meta.data.favorite ? "bg-amber-600" : "bg-slate-800 hover:bg-slate-700")}>
                {meta.data.favorite ? "★ 已收藏" : "☆ 收藏"}
              </button>
              {hasPlaylist && (
                <div className="flex gap-1">
                  <button
                    onClick={() => prev && goToVideo(prev.id)}
                    disabled={!prev}
                    className="flex-1 px-2 py-1 text-xs bg-slate-800 hover:bg-slate-700 disabled:opacity-30 rounded"
                  >上一个</button>
                  <button
                    onClick={() => next && goToVideo(next.id)}
                    disabled={!next}
                    className="flex-1 px-2 py-1 text-xs bg-slate-800 hover:bg-slate-700 disabled:opacity-30 rounded"
                  >下一个</button>
                </div>
              )}
            </div>
          </div>
          <div className="text-xs text-slate-500 flex flex-wrap gap-3">
            {v.width > 0 && <span>{v.width}×{v.height}</span>}
            {v.file_size > 0 && <span>{(v.file_size / 1024 / 1024).toFixed(1)} MB</span>}
            {v.mime_type && <span>{v.mime_type}</span>}
            {v.date && <span>{v.date.slice(0, 19).replace("T", " ")}</span>}
            {v.from && <span>from: {v.from}</span>}
            {containerHint && <span className="text-emerald-500">{containerHint}</span>}
            <Link to="/" className="ml-auto text-slate-400 hover:text-slate-200">← 返回</Link>
          </div>
        </div>
      )}
    </div>
  );
}

function fmtDur(s: number) {
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h) return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return `${m}:${String(sec).padStart(2, "0")}`;
}
