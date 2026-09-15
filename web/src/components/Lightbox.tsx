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
// It never pages on its own. At the last loaded image it offers a "加载下一页"
// button; clicking it fetches one page and moves on to the first new image, if
// that page brought any.
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
  // Set after the user clicks "加载下一页"; when that page lands we step to the
  // first image it added (if any). Nothing here fetches without a click.
  const [awaitingPage, setAwaitingPage] = useState(false);
  const lenBeforeLoad = useRef(items.length);

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
    if (next >= 0 && next < items.length) onIndex(next);
  };

  const loadNextPage = () => {
    if (!onLoadMore || loadingMore) return;
    lenBeforeLoad.current = items.length;
    setAwaitingPage(true);
    onLoadMore();
  };

  // The page the user asked for has landed: move to its first image. A page
  // holding only videos adds none — then stay put and let them click again.
  useEffect(() => {
    if (!awaitingPage || loadingMore) return;
    setAwaitingPage(false);
    if (items.length > lenBeforeLoad.current) onIndex(lenBeforeLoad.current);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [awaitingPage, loadingMore, items.length]);

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
  const nextDisabled = atEnd;
  const showLoadMore = atEnd && canLoadMore;
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
            {item.width > 0 && (
              <span>
                {item.width}×{item.height}
              </span>
            )}
            {item.file_size > 0 && <span>{fmtSize(item.file_size)}</span>}
            {item.date && <span className="tabular-nums">{item.date.slice(0, 10)}</span>}
          </div>
        </div>
        {showLoadMore && (
          <button
            type="button"
            onClick={loadNextPage}
            disabled={loadingMore}
            className="hidden items-center gap-1.5 rounded-full bg-white/10 px-3 py-2 text-theme-xs text-white transition-colors hover:bg-white/20 disabled:opacity-50 sm:inline-flex"
          >
            {loadingMore && <Spinner className="size-4 border-white/30 border-t-white" />}
            加载下一页
          </button>
        )}
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
        {showLoadMore ? (
          <button
            type="button"
            onClick={loadNextPage}
            disabled={loadingMore}
            className="inline-flex items-center gap-1.5 rounded-full bg-white/10 px-4 py-2 text-theme-xs text-white disabled:opacity-50"
          >
            {loadingMore && <Spinner className="size-4 border-white/30 border-t-white" />}
            加载下一页
          </button>
        ) : (
          <span className="text-theme-xs text-white/55">左右滑动切换</span>
        )}
        <button type="button" className={navBtn} disabled={nextDisabled} onClick={() => go(1)}>
          <ChevronRightIcon className="size-6" />
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
