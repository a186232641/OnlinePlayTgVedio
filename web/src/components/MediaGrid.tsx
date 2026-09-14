import { useState } from "react";
import { Link } from "react-router-dom";

import { MediaItem, MediaSource } from "../api/client";
import { ClockIcon, ImageIcon, PlayIcon } from "./icons";
import { EmptyState } from "./ui";

export function fmtDuration(s: number) {
  if (!s) return "";
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h) return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return `${m}:${String(sec).padStart(2, "0")}`;
}

export function fmtSize(bytes: number) {
  if (!bytes) return "";
  const u = ["B", "KB", "MB", "GB"];
  let i = 0;
  let v = bytes;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${u[i]}`;
}

// Thumb renders the tile image, degrading to a typed placeholder. A thumbnail
// is fetched from Telegram on first request, so a miss is normal rather than
// exceptional: plenty of documents carry no thumbnail at all.
function Thumb({ item }: { item: MediaItem }) {
  const [failed, setFailed] = useState(false);
  const isVideo = item.kind === "video";

  if (failed) {
    return (
      <div className="flex size-full items-center justify-center bg-gray-100 text-gray-300 dark:bg-white/[0.04] dark:text-gray-600">
        {isVideo ? <PlayIcon className="size-8" /> : <ImageIcon className="size-8" />}
      </div>
    );
  }
  return (
    <img
      src={item.thumb_url}
      alt=""
      loading="lazy"
      decoding="async"
      onError={() => setFailed(true)}
      className="size-full bg-gray-100 object-cover transition-transform duration-200 group-hover:scale-[1.03] dark:bg-white/[0.04]"
    />
  );
}

// MediaGrid is the shared tile grid for a mixed video+image list.
//
// Videos navigate to the player (via `linkTo`, which callers use to encode the
// playlist context into the URL); images call `onOpenPhoto` so the page can put
// them in the lightbox instead of leaving the list.
// SourceLine links an item back to where it came from: "群组 › 话题" for a
// topic (both parts clickable), just the title for a plain channel.
function SourceLine({ src }: { src: MediaSource }) {
  const link =
    "truncate transition-colors hover:text-brand-600 dark:hover:text-brand-400";
  return (
    <div className="flex min-w-0 items-center gap-1 border-t border-gray-200 px-3 py-2 text-theme-xs text-gray-500 dark:border-gray-800 dark:text-gray-400">
      <span className="shrink-0 text-gray-400">来自</span>
      {src.dialog_kind === "topic" && src.parent_channel_id ? (
        <>
          <Link to={`/channels/${src.parent_channel_id}`} className={link} title={src.parent_title}>
            {src.parent_title}
          </Link>
          <span className="shrink-0 text-gray-300 dark:text-gray-600">›</span>
          <Link to={`/channels/${src.id}`} className={link} title={src.title}>
            {src.title}
          </Link>
        </>
      ) : (
        <Link to={`/channels/${src.id}`} className={link} title={src.title}>
          {src.title}
        </Link>
      )}
    </div>
  );
}

export function MediaGrid({
  items,
  linkTo,
  onOpenPhoto,
  sources,
  emptyLabel = "暂无内容",
}: {
  items: MediaItem[];
  linkTo?: (m: MediaItem) => string;
  onOpenPhoto?: (m: MediaItem) => void;
  // When given (cross-channel lists), each tile gets a link back to its origin.
  sources?: Record<string, MediaSource>;
  emptyLabel?: string;
}) {
  if (items.length === 0) return <EmptyState title={emptyLabel} />;

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 2xl:grid-cols-5 3xl:grid-cols-6">
      {items.map((m) => {
        const label = m.file_name?.trim() || m.text?.trim();
        const tile = (
          <>
            <div className="relative aspect-[4/3] w-full overflow-hidden rounded-t-2xl">
              <Thumb item={m} />
              {m.kind === "video" ? (
                <>
                  <span className="absolute inset-0 flex items-center justify-center opacity-0 transition-opacity group-hover:opacity-100">
                    <span className="flex size-10 items-center justify-center rounded-full bg-black/55 text-white backdrop-blur-sm">
                      <PlayIcon className="size-5" />
                    </span>
                  </span>
                  {m.duration_seconds > 0 && (
                    <span className="absolute bottom-1.5 right-1.5 inline-flex items-center gap-1 rounded-md bg-black/65 px-1.5 py-0.5 text-theme-xs tabular-nums text-white">
                      <ClockIcon className="size-3" />
                      {fmtDuration(m.duration_seconds)}
                    </span>
                  )}
                </>
              ) : (
                <span className="absolute bottom-1.5 right-1.5 inline-flex items-center rounded-md bg-black/65 p-1 text-white">
                  <ImageIcon className="size-3" />
                </span>
              )}
            </div>
            <div className="flex min-w-0 flex-1 flex-col gap-1 p-3">
              <div className="line-clamp-2 break-all text-theme-xs font-medium leading-snug text-gray-800 transition-colors group-hover:text-brand-600 dark:text-white/90 dark:group-hover:text-brand-400">
                {label || <span className="text-gray-400">{m.kind === "video" ? "视频" : "图片"} #{m.id}</span>}
              </div>
              <div className="mt-auto flex flex-wrap items-center gap-x-2 text-theme-xs text-gray-500 dark:text-gray-400">
                {m.file_size > 0 && <span>{fmtSize(m.file_size)}</span>}
                {m.date && <span className="ml-auto tabular-nums">{m.date.slice(0, 10)}</span>}
              </div>
            </div>
          </>
        );

        const src = sources?.[String(m.channel_id)];
        const key = `${m.kind}${m.id}`;
        // The clickable tile and the source links must be siblings: an <a>
        // inside an <a> (or a <button>) is invalid and the inner click is lost.
        const hit = "group flex flex-1 flex-col text-left";
        const body =
          m.kind === "photo" ? (
            <button type="button" onClick={() => onOpenPhoto?.(m)} className={hit}>
              {tile}
            </button>
          ) : (
            <Link to={linkTo ? linkTo(m) : `/videos/${m.id}`} className={hit}>
              {tile}
            </Link>
          );

        return (
          <div
            key={key}
            className="card flex flex-col overflow-hidden p-0 transition-colors hover:border-brand-300 dark:hover:border-brand-500/40"
          >
            {body}
            {src && <SourceLine src={src} />}
          </div>
        );
      })}
    </div>
  );
}
