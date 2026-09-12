import { useMemo, useState } from "react";

import { MediaItem, MediaKindFilter } from "../api/client";
import { Lightbox } from "./Lightbox";
import { MediaGrid } from "./MediaGrid";
import { FilmIcon, GridIcon, ImageIcon } from "./icons";
import { LoadingState, cx } from "./ui";

const KINDS: { value: MediaKindFilter; label: string; Icon: typeof GridIcon }[] = [
  { value: "", label: "全部", Icon: GridIcon },
  { value: "video", label: "视频", Icon: FilmIcon },
  { value: "photo", label: "图片", Icon: ImageIcon },
];

// KindTabs switches a media list between "everything", videos and images. It is
// a server-side filter (?kind=), not a client-side one, so the counts stay
// meaningful across pagination.
export function KindTabs({
  value,
  onChange,
  counts,
}: {
  value: MediaKindFilter;
  onChange: (v: MediaKindFilter) => void;
  counts?: { videos?: number; photos?: number };
}) {
  const count = (v: MediaKindFilter) => {
    if (!counts) return undefined;
    const { videos, photos } = counts;
    if (v === "video") return videos;
    if (v === "photo") return photos;
    if (videos == null && photos == null) return undefined;
    return (videos ?? 0) + (photos ?? 0);
  };
  return (
    <div className="inline-flex rounded-xl bg-gray-100 p-1 dark:bg-white/[0.06]">
      {KINDS.map(({ value: v, label, Icon }) => {
        const n = count(v);
        return (
          <button
            key={v || "all"}
            type="button"
            onClick={() => onChange(v)}
            className={cx(
              "inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-theme-xs font-medium transition-colors",
              value === v
                ? "bg-white text-gray-800 shadow-theme-xs dark:bg-white/[0.08] dark:text-white/90"
                : "text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200",
            )}
          >
            <Icon className="size-4" />
            {label}
            {n != null && <span className="tabular-nums text-gray-400">{n.toLocaleString()}</span>}
          </button>
        );
      })}
    </div>
  );
}

// MediaBrowser pairs the tile grid with the image viewer: clicking an image
// opens the lightbox over the page, and ←/→ there walk only the images of the
// current list (stepping onto a video mid-album would be nonsense).
export function MediaBrowser({
  items,
  isLoading,
  linkTo,
  emptyLabel,
}: {
  items: MediaItem[];
  isLoading?: boolean;
  linkTo?: (m: MediaItem) => string;
  emptyLabel?: string;
}) {
  const photos = useMemo(() => items.filter((i) => i.kind === "photo"), [items]);
  const [openIdx, setOpenIdx] = useState<number | null>(null);

  if (isLoading) return <LoadingState />;
  return (
    <>
      <MediaGrid
        items={items}
        linkTo={linkTo}
        emptyLabel={emptyLabel}
        onOpenPhoto={(m) => {
          const i = photos.findIndex((p) => p.id === m.id);
          if (i >= 0) setOpenIdx(i);
        }}
      />
      {openIdx !== null && (
        <Lightbox
          items={photos}
          index={openIdx}
          onIndex={setOpenIdx}
          onClose={() => setOpenIdx(null)}
        />
      )}
    </>
  );
}
