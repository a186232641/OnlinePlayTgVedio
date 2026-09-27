import { useEffect, useRef, useState } from "react";

import { MediaItem } from "../api/client";
import { useFavoriteState, useToggleFavorite } from "../favState";
import { attachStream } from "../playback";
import { ChevronLeftIcon, ChevronRightIcon, CloseIcon, PauseIcon, PlayIcon, StarIcon } from "./icons";
import { Spinner, cx } from "./ui";

// How long an image stays up in auto mode.
export const IMAGE_DWELL_MS = 2500;

// Slideshow plays a mixed list — images and videos — full screen, in list
// order. Switching is manual (←/→, buttons, swipe) unless "自动播放" is on: then
// an image stays IMAGE_DWELL_MS and a video advances when it ends, i.e. after
// its own duration.
//
// Like the image viewer and the player, it never pages on its own: at the last
// loaded item it stops and offers "加载下一页", which fetches one page and moves
// on to the first item that page added.
export function Slideshow({
  items,
  startIndex = 0,
  onClose,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
}: {
  items: MediaItem[];
  startIndex?: number;
  onClose: () => void;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
}) {
  const [index, setIndex] = useState(startIndex);
  const [auto, setAuto] = useState(true);
  // Which image has loaded (or failed): only then does its dwell start, so a
  // slow image still gets its full 2.5s on screen. Keyed by item rather than a
  // boolean reset on change, so a new item is never "ready" for one render.
  const [readyKey, setReadyKey] = useState<string | null>(null);
  const [videoErr, setVideoErr] = useState<string | null>(null);
  const [awaitingPage, setAwaitingPage] = useState(false);
  const lenBeforeLoad = useRef(items.length);
  const videoRef = useRef<HTMLVideoElement | null>(null);

  const item = items[index];
  const itemKey = item ? `${item.kind}:${item.id}` : "";
  const imgReady = readyKey === itemKey;
  const atEnd = index >= items.length - 1;
  const finished = atEnd && !hasMore; // nothing left anywhere

  const go = (delta: number) => {
    const next = index + delta;
    if (next >= 0 && next < items.length) setIndex(next);
  };
  // For timers: step forward from whatever is current when they fire, so they
  // needn't be re-armed on every render (which would restart the dwell).
  const lenRef = useRef(items.length);
  lenRef.current = items.length;
  const advance = () => setIndex((i) => (i < lenRef.current - 1 ? i + 1 : i));

  const loadNextPage = () => {
    if (!onLoadMore || loadingMore) return;
    lenBeforeLoad.current = items.length;
    setAwaitingPage(true);
    onLoadMore();
  };
  // Step onto the first item of the page the button fetched.
  useEffect(() => {
    if (!awaitingPage || loadingMore) return;
    setAwaitingPage(false);
    if (items.length > lenBeforeLoad.current) setIndex(lenBeforeLoad.current);
  }, [awaitingPage, loadingMore, items.length]);

  useEffect(() => {
    setVideoErr(null);
  }, [itemKey]);

  // Auto mode, image: advance after the dwell. (A video advances from onEnded.)
  const isPhoto = item?.kind === "photo";
  useEffect(() => {
    if (!auto || !isPhoto || !imgReady || atEnd) return;
    const t = setTimeout(advance, IMAGE_DWELL_MS);
    return () => clearTimeout(t);
  }, [auto, isPhoto, imgReady, atEnd, index]); // eslint-disable-line react-hooks/exhaustive-deps

  // Video: attach the stream (mpegts.js for FLV/TS, else native) and play.
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !item || item.kind !== "video") return;
    // retryMuted: a blocked autoplay would leave the video paused and stall
    // auto mode, so fall back to playing muted.
    return attachStream(video, item.url, { onError: (msg) => setVideoErr(msg), retryMuted: true });
  }, [item?.kind, item?.id, item?.url]); // eslint-disable-line react-hooks/exhaustive-deps

  // A video that won't play would stall auto mode — skip it after a moment.
  useEffect(() => {
    if (!auto || !videoErr || atEnd) return;
    const t = setTimeout(advance, IMAGE_DWELL_MS);
    return () => clearTimeout(t);
  }, [auto, videoErr, atEnd, index]); // eslint-disable-line react-hooks/exhaustive-deps

  // Preload the neighbouring images.
  useEffect(() => {
    for (const n of [items[index + 1], items[index - 1]]) {
      if (n?.kind === "photo") new Image().src = n.url;
    }
  }, [index, items]);

  // Swipe on touch screens.
  const touch = useRef<{ x: number; y: number } | null>(null);
  const onTouchStart = (e: React.TouchEvent) => {
    const t = e.touches[0];
    touch.current = { x: t.clientX, y: t.clientY };
  };
  const onTouchEnd = (e: React.TouchEvent) => {
    const start = touch.current;
    touch.current = null;
    if (!start) return;
    const t = e.changedTouches[0];
    const dx = t.clientX - start.x;
    const dy = t.clientY - start.y;
    if (Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy) * 1.5) go(dx < 0 ? 1 : -1);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // On a focused <video>, arrows seek and space plays — leave those to it.
      const t = e.target as HTMLElement | null;
      if (t && ["INPUT", "TEXTAREA", "VIDEO"].includes(t.tagName) && e.key !== "Escape") return;
      if (e.key === "Escape") onClose();
      else if (e.key === "ArrowLeft") go(-1);
      else if (e.key === "ArrowRight") go(1);
      else if (e.key === " ") {
        e.preventDefault();
        setAuto((a) => !a);
      }
    };
    window.addEventListener("keydown", onKey);
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
    };
  }); // re-bound every render so the handler closes over the current index

  const fav = useFavoriteState(item?.kind ?? "photo", item?.id, !!item?.favorite);
  const toggleFav = useToggleFavorite();

  if (!item) return null;

  const navBtn =
    "flex size-11 items-center justify-center rounded-full bg-white/10 text-white backdrop-blur-sm transition-colors hover:bg-white/20 disabled:cursor-not-allowed disabled:opacity-25";
  const pill =
    "inline-flex items-center gap-1.5 rounded-full bg-white/10 px-3 py-2 text-theme-xs text-white transition-colors hover:bg-white/20 disabled:opacity-50";

  // What to show once the loaded list is used up.
  const endNote = atEnd
    ? hasMore
      ? (
          <button type="button" onClick={loadNextPage} disabled={loadingMore} className={pill}>
            {loadingMore && <Spinner className="size-4 border-white/30 border-t-white" />}
            已到已加载的末尾 · 加载下一页
          </button>
        )
      : <span className="text-theme-xs text-white/55">已是最后一项</span>
    : null;

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-black" role="dialog" aria-modal="true">
      {/* Top bar */}
      <div className="flex items-center gap-3 px-4 py-3 text-white">
        <div className="min-w-0 flex-1">
          <div className="truncate text-theme-sm font-medium">
            {item.file_name?.trim() || item.text?.trim() || `${item.kind === "video" ? "视频" : "图片"} #${item.id}`}
          </div>
          <div className="mt-0.5 text-theme-xs tabular-nums text-white/55">
            {index + 1} / {items.length}
            {hasMore ? "+" : ""}
            {auto && !finished && (item.kind === "photo" ? ` · 每张 ${IMAGE_DWELL_MS / 1000}s` : " · 播完自动下一个")}
          </div>
        </div>
        <button
          type="button"
          onClick={() => setAuto((a) => !a)}
          title="自动播放 (空格)"
          className={cx(pill, auto && "bg-brand-500 hover:bg-brand-600")}
        >
          {auto ? <PauseIcon className="size-4" /> : <PlayIcon className="size-4" />}
          {auto ? "自动播放中" : "手动切换"}
        </button>
        <button
          type="button"
          onClick={() => toggleFav.mutate({ kind: item.kind, id: item.id, fav })}
          disabled={toggleFav.isPending}
          title={fav ? "取消收藏" : "收藏"}
          className={cx(
            "flex size-10 items-center justify-center rounded-full transition-colors",
            fav ? "bg-warning-500 text-white" : "bg-white/10 text-white hover:bg-white/20",
          )}
        >
          <StarIcon filled={fav} className="size-5" />
        </button>
        <button
          type="button"
          onClick={onClose}
          title="关闭 (Esc)"
          className="flex size-10 items-center justify-center rounded-full bg-white/10 text-white transition-colors hover:bg-white/20"
        >
          <CloseIcon className="size-5" />
        </button>
      </div>

      {/* Image dwell progress (auto mode). Keyed by item so it restarts. */}
      <div className="h-0.5 bg-white/10">
        {auto && item.kind === "photo" && imgReady && !atEnd && (
          <div
            key={`${item.kind}${item.id}`}
            className="h-full origin-left bg-brand-500"
            style={{ animation: `slideshow-progress ${IMAGE_DWELL_MS}ms linear forwards` }}
          />
        )}
      </div>

      {/* Stage */}
      <div
        className="flex min-h-0 flex-1 items-center gap-2 px-2 py-3 sm:gap-4 sm:px-4"
        onTouchStart={onTouchStart}
        onTouchEnd={onTouchEnd}
      >
        <button type="button" className={cx(navBtn, "hidden sm:flex")} disabled={index <= 0} onClick={() => go(-1)} title="上一个 (←)">
          <ChevronLeftIcon className="size-6" />
        </button>

        <div className="relative flex min-w-0 flex-1 items-center justify-center self-stretch">
          {item.kind === "photo" ? (
            <>
              {!imgReady && <Spinner className="absolute size-6 border-white/30 border-t-white" />}
              <img
                key={item.id}
                src={item.url}
                alt=""
                onLoad={() => setReadyKey(itemKey)}
                onError={() => setReadyKey(itemKey)}
                className={cx(
                  "max-h-full max-w-full object-contain transition-opacity",
                  imgReady ? "opacity-100" : "opacity-0",
                )}
              />
            </>
          ) : (
            <>
              <video
                key={item.id}
                ref={videoRef}
                controls
                playsInline
                className="max-h-full max-w-full outline-none"
                onEnded={() => {
                  if (auto) go(1);
                }}
                onError={() => setVideoErr("视频无法播放")}
              />
              {videoErr && (
                <div className="absolute inset-x-0 bottom-0 bg-black/80 p-3 text-center text-theme-xs text-error-300">
                  播放失败: {videoErr}
                  {auto && !atEnd && " · 即将跳到下一个"}
                </div>
              )}
            </>
          )}
        </div>

        <button type="button" className={cx(navBtn, "hidden sm:flex")} disabled={atEnd} onClick={() => go(1)} title="下一个 (→)">
          <ChevronRightIcon className="size-6" />
        </button>
      </div>

      {/* Bottom: phone nav + end-of-list action */}
      <div className="flex min-h-14 items-center justify-between gap-3 px-4 pb-3">
        <button type="button" className={cx(navBtn, "sm:invisible")} disabled={index <= 0} onClick={() => go(-1)}>
          <ChevronLeftIcon className="size-6" />
        </button>
        <div className="flex min-w-0 flex-1 justify-center">
          {endNote ?? <span className="hidden text-theme-xs text-white/40 sm:inline">← / → 切换 · 空格 开关自动播放 · Esc 退出</span>}
        </div>
        <button type="button" className={cx(navBtn, "sm:invisible")} disabled={atEnd} onClick={() => go(1)}>
          <ChevronRightIcon className="size-6" />
        </button>
      </div>
    </div>
  );
}
