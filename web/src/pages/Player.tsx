import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import mpegts from "mpegts.js";

import { api, Video } from "../api/client";
import { ChevronLeftIcon, ChevronRightIcon, PlayIcon, StarIcon } from "../components/icons";
import { LoadingState, Spinner, cx } from "../components/ui";

interface VideoResp { video: Video; favorite: boolean }
interface VideosResp { videos: Video[] }

const MEDIA_ERR_LABEL: Record<number, string> = {
  1: "ABORTED 用户取消加载",
  2: "NETWORK 网络层错误(后端 502/503/连接断开)",
  3: "DECODE 解码失败(浏览器不支持该编码,如 H.265)",
  4: "SRC_NOT_SUPPORTED 服务端返回不能识别为视频",
};

const PLAYLIST_PAGE_SIZE = 500;

// playlistRequest builds the base URL (without offset_id) based on URL params:
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
  // `order` is forwarded to every list endpoint so the playlist ordering
  // matches the page the user came from (and prev/next walk in that order).
  const order = p.get("order");

  if (fav) {
    const qs = new URLSearchParams({ limit: String(PLAYLIST_PAGE_SIZE) });
    if (fileName) qs.set("file_name", fileName);
    if (dateFrom) qs.set("date_from", dateFrom);
    if (dateTo) qs.set("date_to", dateTo);
    if (order) qs.set("order", order);
    return `/api/favorites/?${qs}`;
  }

  const hasSearch = !!(q || text || fileName || dateFrom || dateTo);
  if (hasSearch) {
    const qs = new URLSearchParams({ limit: String(PLAYLIST_PAGE_SIZE) });
    if (q) qs.set("q", q);
    if (text) qs.set("text", text);
    if (fileName) qs.set("file_name", fileName);
    if (dateFrom) qs.set("date_from", dateFrom);
    if (dateTo) qs.set("date_to", dateTo);
    if (ch) qs.set("channel_id", ch);
    if (order) qs.set("order", order);
    return `/api/videos/search?${qs}`;
  }
  if (ch) {
    const qs = new URLSearchParams({ limit: String(PLAYLIST_PAGE_SIZE) });
    // Grouped channel view links carry ?streamer=... (possibly empty = the
    // "其它" bucket) — keep the playlist scoped to that streamer.
    if (p.has("streamer")) qs.set("streamer", p.get("streamer") ?? "");
    if (order) qs.set("order", order);
    return `/api/channels/${ch}/videos?${qs}`;
  }
  return null;
}

// withOffset appends offset_id=N as a keyset cursor.
function withOffset(url: string, offsetID: number): string {
  if (offsetID <= 0) return url;
  const sep = url.includes("?") ? "&" : "?";
  return `${url}${sep}offset_id=${offsetID}`;
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
  const sidebarRef = useRef<HTMLDivElement | null>(null);

  // Stable string of the search params; used both as react-query cache key
  // and as a dependency for the "scroll-into-view" effect.
  const playlistKey = searchParams.toString();

  useEffect(() => {
    setMediaErr(null);
    setStreamDiag(null);
    setContainerHint("");
  }, [id]);

  // Position the highlighted item inside the sidebar — but only by setting
  // the scroller's scrollTop directly, never via scrollIntoView (which can
  // cascade up to ancestor scroll containers and yank the whole page to the
  // top). Strategy: if the current item's box is already inside the visible
  // window, do nothing. Otherwise center it. Offsets are measured from the
  // client rects so nested markup / positioned ancestors can't skew the math.
  useEffect(() => {
    const scroller = sidebarRef.current;
    if (!scroller || !id) return;
    const el = scroller.querySelector<HTMLElement>(`[data-video-id="${id}"]`);
    if (!el) return;
    const top =
      el.getBoundingClientRect().top -
      scroller.getBoundingClientRect().top +
      scroller.scrollTop;
    const bottom = top + el.offsetHeight;
    const viewTop = scroller.scrollTop;
    const viewBottom = viewTop + scroller.clientHeight;
    if (top >= viewTop && bottom <= viewBottom) {
      return; // already visible — leave the user's scroll position alone
    }
    scroller.scrollTop = Math.max(0, top - scroller.clientHeight / 2 + el.offsetHeight / 2);
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

  // Playlist (siblings from the same context). useInfiniteQuery so we can
  // page past 500 items as the user scrolls / autoplays towards the bottom.
  const baseURL = playlistRequest(searchParams);
  const playlist = useInfiniteQuery<VideosResp>({
    queryKey: ["playlist", playlistKey],
    enabled: !!baseURL,
    initialPageParam: 0,
    queryFn: ({ pageParam }) => {
      if (!baseURL) return Promise.resolve({ videos: [] });
      return api.get<VideosResp>(withOffset(baseURL, pageParam as number));
    },
    getNextPageParam: (last) => {
      if (last.videos.length < PLAYLIST_PAGE_SIZE) return undefined;
      return last.videos[last.videos.length - 1]?.id;
    },
  });

  const list = useMemo<Video[]>(
    () => playlist.data?.pages.flatMap((p) => p.videos) ?? [],
    [playlist.data],
  );
  const currentIdx = useMemo(
    () => list.findIndex((v) => String(v.id) === id),
    [list, id],
  );
  const next = currentIdx >= 0 && currentIdx < list.length - 1 ? list[currentIdx + 1] : null;
  const prev = currentIdx > 0 ? list[currentIdx - 1] : null;

  // Pre-fetch the next page when the current item is within the last 50 of
  // what's loaded — keeps autoplay smooth across page boundaries.
  useEffect(() => {
    if (currentIdx < 0 || !playlist.hasNextPage || playlist.isFetchingNextPage) return;
    if (currentIdx >= list.length - 50) {
      playlist.fetchNextPage();
    }
  }, [currentIdx, list.length, playlist.hasNextPage, playlist.isFetchingNextPage, playlist.fetchNextPage]);

  // IntersectionObserver at the bottom of the sidebar: load more on scroll.
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = sentinelRef.current;
    const root = sidebarRef.current;
    if (!el || !root) return;
    const obs = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting && playlist.hasNextPage && !playlist.isFetchingNextPage) {
          playlist.fetchNextPage();
        }
      },
      { root, rootMargin: "200px" },
    );
    obs.observe(el);
    return () => obs.disconnect();
  }, [playlist.hasNextPage, playlist.isFetchingNextPage, playlist.fetchNextPage, list.length]);

  // Container detection + player setup
  useEffect(() => {
    const video = videoRef.current;
    const url = meta.data?.video.stream_url;
    if (!video || !url) return;

    let cancelled = false;
    setMediaErr(null);
    setStreamDiag(null);

    (async () => {
      let kind: "flv" | "mpegts" | "native" = "native";
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
          } else if (b.length >= 1 && b[0] === 0x47) {
            // MPEG-TS sync byte (0x47) at offset 0 — naked .ts stream.
            kind = "mpegts";
            hint = "MPEG-TS — mpegts.js";
          } else {
            hint = "未识别容器,试试 native";
          }
        }
      } catch {
        // probe failed
      }
      if (cancelled) return;
      setContainerHint(hint);

      if (kind === "flv" || kind === "mpegts") {
        if (!mpegts.getFeatureList().mseLivePlayback) {
          setMediaErr("浏览器不支持 MSE — 该视频(FLV/TS)无法播放");
          return;
        }
        const player = mpegts.createPlayer({
          type: kind, // "flv" | "mpegts"
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
  const isFav = !!meta.data?.favorite;

  return (
    <div className="p-4 md:p-6">
      <div className={cx("grid gap-5", hasPlaylist && "xl:grid-cols-[minmax(0,1fr)_360px]")}>
        <div className="min-w-0 space-y-5">
          {/* video stage — media keeps its own near-black backdrop inside the
              card frame; the chrome around it stays on the neutral canvas. */}
          <div className="card overflow-hidden">
            <div className="relative flex min-h-[40vh] items-center justify-center bg-gray-950">
              {showLoading && <LoadingState />}
              {showNotFound && (
                <div className="p-6 text-theme-sm text-error-400">未找到视频</div>
              )}
              {v && (
                <>
                  <video
                    key={v.id}
                    ref={videoRef}
                    controls
                    preload="metadata"
                    className="max-h-[78vh] max-w-full outline-none"
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
                    <div className="absolute inset-x-0 bottom-0 space-y-1 border-l-4 border-error-500 bg-gray-900/95 p-3 text-theme-xs text-error-300 backdrop-blur">
                      <div className="font-medium">播放失败: {mediaErr}</div>
                      {streamDiag && (
                        <pre className="whitespace-pre-wrap font-mono text-[11px] text-gray-400">
                          {streamDiag}
                        </pre>
                      )}
                      <div className="break-all text-gray-500">stream_url: {v.stream_url}</div>
                    </div>
                  )}
                </>
              )}
            </div>
          </div>

          {/* metadata + controls */}
          {v && meta.data && (
            <div className="card space-y-4 p-5">
              <div className="flex flex-wrap items-start gap-4">
                <div className="min-w-[240px] flex-1 space-y-1.5">
                  {v.file_name && (
                    <div className="break-all font-medium text-gray-800 dark:text-white/90">
                      {v.file_name}
                    </div>
                  )}
                  <div className="whitespace-pre-wrap break-words text-theme-sm text-gray-600 dark:text-gray-300">
                    {v.text?.trim() || <span className="text-gray-400">无说明</span>}
                  </div>
                </div>

                <div className="flex shrink-0 flex-col gap-2">
                  <button
                    onClick={() => fav.mutate()}
                    disabled={fav.isPending}
                    className={cx(
                      "btn",
                      isFav
                        ? "bg-brand-50 text-brand-500 ring-1 ring-inset ring-brand-200 hover:bg-brand-100 dark:bg-brand-500/[0.12] dark:text-brand-400 dark:ring-brand-500/30"
                        : "btn-primary",
                    )}
                  >
                    <StarIcon filled={isFav} className="size-4" />
                    {isFav ? "已收藏" : "收藏"}
                  </button>
                  {hasPlaylist && (
                    <div className="flex gap-2">
                      <button
                        onClick={() => prev && goToVideo(prev.id)}
                        disabled={!prev}
                        className="btn btn-outline btn-sm flex-1"
                      >
                        <ChevronLeftIcon className="size-4" />
                        上一个
                      </button>
                      <button
                        onClick={() => next && goToVideo(next.id)}
                        disabled={!next}
                        className="btn btn-outline btn-sm flex-1"
                      >
                        下一个
                        <ChevronRightIcon className="size-4" />
                      </button>
                    </div>
                  )}
                </div>
              </div>

              <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t border-gray-200 pt-3 text-theme-xs text-gray-500 dark:border-gray-800 dark:text-gray-400">
                {v.width > 0 && <span>{v.width}×{v.height}</span>}
                {v.file_size > 0 && <span>{(v.file_size / 1024 / 1024).toFixed(1)} MB</span>}
                {v.mime_type && <span>{v.mime_type}</span>}
                {v.date && <span className="tabular-nums">{v.date.slice(0, 19).replace("T", " ")}</span>}
                {v.from && <span>from: {v.from}</span>}
                {containerHint && <span className="badge badge-info">{containerHint}</span>}
                <Link
                  to="/"
                  className="ml-auto inline-flex items-center gap-1 hover:text-gray-700 dark:hover:text-gray-200"
                >
                  <ChevronLeftIcon className="size-4" />
                  返回频道
                </Link>
              </div>
            </div>
          )}
        </div>

        {/* playlist sidebar */}
        {hasPlaylist && (
          <aside className="card flex max-h-[80vh] min-h-0 flex-col overflow-hidden xl:sticky xl:top-[84px]">
            <div className="flex shrink-0 items-center gap-2 border-b border-gray-200 px-4 py-3 dark:border-gray-800">
              <span className="text-theme-sm font-medium text-gray-800 dark:text-white/90">
                播放列表
              </span>
              <span className="badge badge-gray tabular-nums">
                {currentIdx + 1}/{list.length}
              </span>
              {playlist.hasNextPage && (
                <span className="ml-auto text-theme-xs text-gray-400 dark:text-gray-500">
                  滚动加载更多
                </span>
              )}
            </div>

            <div ref={sidebarRef} className="custom-scrollbar min-h-0 flex-1 overflow-y-auto">
              {list.map((item, i) => {
                const isCurrent = String(item.id) === id;
                return (
                  <button
                    key={item.id}
                    data-video-id={item.id}
                    onClick={() => goToVideo(item.id)}
                    className={cx(
                      "flex w-full items-start gap-2.5 border-b border-gray-100 px-4 py-2.5 text-left transition-colors dark:border-gray-800/60",
                      isCurrent
                        ? "bg-brand-50 dark:bg-brand-500/[0.12]"
                        : "hover:bg-gray-50 dark:hover:bg-white/[0.04]",
                    )}
                  >
                    <span
                      className={cx(
                        "mt-0.5 w-6 shrink-0 text-theme-xs tabular-nums",
                        isCurrent
                          ? "text-brand-500 dark:text-brand-400"
                          : "text-gray-400 dark:text-gray-500",
                      )}
                    >
                      {isCurrent ? <PlayIcon className="size-3" /> : i + 1}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div
                        className={cx(
                          "line-clamp-2 break-all text-theme-xs leading-snug",
                          isCurrent
                            ? "font-medium text-brand-600 dark:text-brand-400"
                            : "text-gray-700 dark:text-gray-300",
                        )}
                      >
                        {item.file_name?.trim() || item.text?.trim() || `视频 #${item.id}`}
                      </div>
                      <div className="mt-0.5 text-[11px] leading-4 text-gray-400 dark:text-gray-500">
                        {item.duration_seconds > 0 && fmtDur(item.duration_seconds)}
                        {item.date && " · " + item.date.slice(0, 10)}
                      </div>
                    </div>
                  </button>
                );
              })}

              <div
                ref={sentinelRef}
                className="flex items-center justify-center gap-2 py-3 text-theme-xs text-gray-400 dark:text-gray-500"
              >
                {playlist.isFetchingNextPage ? (
                  <>
                    <Spinner className="size-3.5" />
                    加载更多…
                  </>
                ) : playlist.hasNextPage ? (
                  <button
                    onClick={() => playlist.fetchNextPage()}
                    className="hover:text-gray-600 dark:hover:text-gray-300"
                  >
                    加载更多
                  </button>
                ) : list.length > 0 ? (
                  "— 已加载全部 —"
                ) : null}
              </div>
            </div>
          </aside>
        )}
      </div>
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
