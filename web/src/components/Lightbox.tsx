import { useEffect, useState } from "react";
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
// and ←/→ walk the same list the grid is showing.
export function Lightbox({
  items,
  index,
  onIndex,
  onClose,
}: {
  items: MediaItem[];
  index: number;
  onIndex: (i: number) => void;
  onClose: () => void;
}) {
  const item = items[index];
  const qc = useQueryClient();
  const [loaded, setLoaded] = useState(false);

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

  const go = (delta: number) => {
    const next = index + delta;
    if (next >= 0 && next < items.length) onIndex(next);
  };

  useEffect(() => {
    setLoaded(false);
  }, [item?.id]);

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
  const nextDisabled = index >= items.length - 1;
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
      <div className="flex min-h-0 flex-1 items-center gap-2 px-2 pb-4 sm:gap-4 sm:px-4">
        <button
          type="button"
          className={navBtn}
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
          className={navBtn}
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
