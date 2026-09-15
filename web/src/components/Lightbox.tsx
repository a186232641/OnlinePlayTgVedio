import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, MediaItem } from "../api/client";
import { ChevronLeftIcon, ChevronRightIcon, CloseIcon, StarIcon } from "../components/icons";
import { Spinner, cx } from "./ui";
import { fmtSize } from "./MediaGrid";

interface PhotoResp {
  photo: MediaItem;
  favorite: boolean;
}

// Lightbox is the full-screen image viewer. Images never leave the list page —
// unlike a video, there is nothing to navigate to — so browsing stays in place
// and ←/→ (or a swipe) walk the same list the grid is showing.
//
// Like the video player's playlist, it does not stop at the end of what the
// grid happens to have loaded: stepping past the last loaded image asks the
// list for its next page and continues once it arrives.
export function Lightbox({
  items,
  index,
  onIndex,
  onClose,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
}: {
  items: MediaItem[];
  index: number;
  onIndex: (i: number) => void;
  onClose: () => void;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
}) {
  const item = items[index];
  const qc = useQueryClient();
  const [loaded, setLoaded] = useState(false);
  // Set when the user asked for "next" at the end of the loaded images; cleared
  // once the next image exists or the list is exhausted.
  const [pendingNext, setPendingNext] = useState(false);
  // A page can contain only videos, adding no image; keep fetching for the
  // user's "next" but give up after this many empty pages in a row.
  const emptyPages = useRef(0);
  const lastLen = useRef(items.length);

  // Favorite state comes from the photo detail endpoint (same shape as the
  // player's), so the star reflects reality after a reload.
  const meta = useQuery<PhotoResp>({
    queryKey: ["photo", item?.id],
    queryFn: () => api.get(`/api/photos/${item.id}`),
    enabled: !!item,
  });
  const favorite = !!meta.data?.favorite;

  const fav = useMutation({
    mutationFn: async () => {
      if (favorite) return api.del(`/api/favorites/photo/${item.id}`);
      return api.post("/api/favorites/", { kind: "photo", id: item.id });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["photo", item.id] });
      qc.invalidateQueries({ queryKey: ["favorites"] });
    },
    onError: (e: Error) => alert(`收藏操作失败: ${e.message}`),
  });

  const atEnd = index >= items.length - 1;
  const canLoadMore = hasMore && !!onLoadMore;

  const go = (delta: number) => {
    const next = index + delta;
    if (next < 0) return;
    if (next < items.length) {
      onIndex(next);
      return;
    }
    if (delta > 0 && canLoadMore) {
      // Only flag it; the effect below does the fetching, so a click can't race
      // it into requesting the same page twice.
      emptyPages.current = 0;
      setPendingNext(true);
    }
  };

  // A page arrived: either it brought the image the user is waiting for, or it
  // brought none (all videos) and we fetch again — bounded.
  useEffect(() => {
    const grew = items.length > lastLen.current;
    lastLen.current = items.length;
    if (!pendingNext || loadingMore) return;
    if (index + 1 < items.length) {
      setPendingNext(false);
      onIndex(index + 1);
      return;
    }
    if (!grew) emptyPages.current += 1;
    if (canLoadMore && emptyPages.current < 10) {
      onLoadMore?.();
    } else {
      setPendingNext(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items.length, loadingMore, pendingNext]);

  // Prefetch the next page when the viewer gets close to the end, so stepping
  // forward rarely has to wait — the same trick the video playlist uses. Once
  // per list length, so an image-less page doesn't trigger a fetch cascade.
  const prefetchedAt = useRef(-1);
  useEffect(() => {
    if (!canLoadMore || loadingMore) return;
    if (index >= items.length - 3 && prefetchedAt.current !== items.length) {
      prefetchedAt.current = items.length;
      onLoadMore?.();
    }
  }, [index, items.length, canLoadMore, loadingMore, onLoadMore]);

  // Warm the browser cache for both neighbours so a step is instant.
  useEffect(() => {
    for (const n of [items[index + 1], items[index - 1]]) {
      if (n) new Image().src = n.url;
    }
  }, [index, items]);

  useEffect(() => {
    setLoaded(false);
  }, [item?.id]);

  // Horizontal swipe on touch screens: a mostly-sideways drag past a threshold.
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
      if (e.key === "Escape") onClose();
      if (e.key === "ArrowLeft") go(-1);
      if (e.key === "ArrowRight") go(1);
    };
    window.addEventListener("keydown", onKey);
    // Freeze the page behind the overlay.
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
    };
  }); // deliberately re-bound on every render so the key handler closes over the current index

  if (!item) return null;

  const prevDisabled = index <= 0;
  const nextDisabled = (atEnd && !canLoadMore) || pendingNext;
  const navBtn =
    "flex size-11 items-center justify-center rounded-full bg-white/10 text-white backdrop-blur-sm transition-colors hover:bg-white/20 disabled:cursor-not-allowed disabled:opacity-25";

  return (
    <div
      className="fixed inset-0 z-50 flex flex-col bg-black/90"
      role="dialog"
      aria-modal="true"
      onClick={onClose}
    >
      {/* Top bar: caption + actions. */}
      <div
        className="flex items-center gap-3 px-4 py-3 text-white"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="min-w-0 flex-1">
          <div className="truncate text-theme-sm font-medium">
            {item.file_name?.trim() || item.text?.trim() || `图片 #${item.id}`}
          </div>
          <div className="mt-0.5 flex flex-wrap gap-x-3 text-theme-xs text-white/55">
            <span className="tabular-nums">
              {index + 1} / {items.length}
              {hasMore ? "+" : ""}
            </span>
            {pendingNext && <span>加载下一页…</span>}
            {item.width > 0 && (
              <span>
                {item.width}×{item.height}
              </span>
            )}
            {item.file_size > 0 && <span>{fmtSize(item.file_size)}</span>}
            {item.date && <span className="tabular-nums">{item.date.slice(0, 10)}</span>}
          </div>
        </div>
        <button
          type="button"
          onClick={() => fav.mutate()}
          disabled={fav.isPending || meta.isLoading}
          title={favorite ? "取消收藏" : "收藏"}
          className={cx(
            "flex size-10 items-center justify-center rounded-full transition-colors",
            favorite ? "bg-warning-500 text-white" : "bg-white/10 text-white hover:bg-white/20",
          )}
        >
          <StarIcon filled={favorite} className="size-5" />
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

      {/* Stage. */}
      <div
        className="flex min-h-0 flex-1 items-center gap-2 px-2 pb-4 sm:gap-4 sm:px-4"
        onTouchStart={onTouchStart}
        onTouchEnd={onTouchEnd}
      >
        <button
          type="button"
          className={cx(navBtn, "hidden sm:flex")}
          disabled={prevDisabled}
          onClick={(e) => {
            e.stopPropagation();
            go(-1);
          }}
          title="上一张 (←)"
        >
          <ChevronLeftIcon className="size-6" />
        </button>

        <div className="relative flex min-w-0 flex-1 items-center justify-center self-stretch">
          {!loaded && <Spinner className="absolute size-6 border-white/30 border-t-white" />}
          <img
            src={item.url}
            alt=""
            onClick={(e) => e.stopPropagation()}
            onLoad={() => setLoaded(true)}
            onError={() => setLoaded(true)}
            className={cx(
              "max-h-full max-w-full object-contain transition-opacity",
              loaded ? "opacity-100" : "opacity-0",
            )}
          />
        </div>

        <button
          type="button"
          className={cx(navBtn, "hidden sm:flex")}
          disabled={nextDisabled}
          onClick={(e) => {
            e.stopPropagation();
            go(1);
          }}
          title="下一张 (→)"
        >
          <ChevronRightIcon className="size-6" />
        </button>
      </div>

      {/* Phones: the side buttons would eat the image's width, so prev/next
          sit under it (swiping works too). */}
      <div
        className="flex items-center justify-between gap-3 px-4 pb-3 sm:hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <button type="button" className={navBtn} disabled={prevDisabled} onClick={() => go(-1)}>
          <ChevronLeftIcon className="size-6" />
        </button>
        <span className="text-theme-xs text-white/55">左右滑动切换</span>
        <button type="button" className={navBtn} disabled={nextDisabled} onClick={() => go(1)}>
          {pendingNext ? <Spinner className="size-5 border-white/30 border-t-white" /> : <ChevronRightIcon className="size-6" />}
        </button>
      </div>

      {item.text?.trim() && (
        <div
          className="max-h-24 overflow-y-auto border-t border-white/10 px-4 py-3 text-theme-xs text-white/70"
          onClick={(e) => e.stopPropagation()}
        >
          {item.text}
        </div>
      )}
    </div>
  );
}
