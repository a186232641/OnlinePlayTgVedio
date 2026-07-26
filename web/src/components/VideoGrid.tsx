import { Link } from "react-router-dom";

import { Video } from "../api/client";
import { ClockIcon, PlayIcon } from "./icons";
import { EmptyState } from "./ui";

function fmtDuration(s: number) {
  if (!s) return "—";
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h) return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return `${m}:${String(sec).padStart(2, "0")}`;
}

function fmtSize(bytes: number) {
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

function fmtDate(s?: string) {
  if (!s) return "";
  return s.slice(0, 10);
}

// Pure-text card with file_name as the headline (caption second, since TG
// Desktop captions are often empty / hashtag-only).
//
// `linkTo`: optional builder so callers can encode playlist context into the
// URL (e.g. /videos/X?ch=13 — the player picks that up to build a playlist).
export function VideoGrid({
  videos,
  linkTo,
  emptyLabel = "暂无视频",
}: {
  videos: Video[];
  linkTo?: (v: Video) => string;
  emptyLabel?: string;
}) {
  if (videos.length === 0) return <EmptyState title={emptyLabel} />;
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3 3xl:grid-cols-4">
      {videos.map((v) => (
        <Link
          key={v.id}
          to={linkTo ? linkTo(v) : `/videos/${v.id}`}
          className="card group flex min-h-[124px] flex-col gap-2 p-4 transition-colors hover:border-brand-300 hover:bg-brand-25 dark:hover:border-brand-500/40 dark:hover:bg-brand-500/[0.06]"
        >
          <div className="flex items-start gap-2.5">
            <span className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-gray-400 transition-colors group-hover:bg-brand-500 group-hover:text-white dark:bg-white/[0.06] dark:text-gray-500">
              <PlayIcon className="size-3.5" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="line-clamp-2 break-all text-theme-sm font-medium leading-snug text-gray-800 transition-colors group-hover:text-brand-600 dark:text-white/90 dark:group-hover:text-brand-400">
                {v.file_name?.trim() || v.text?.trim() || (
                  <span className="text-gray-400">视频 #{v.id}</span>
                )}
              </div>
              {v.file_name && v.text && (
                <div className="mt-1 line-clamp-2 text-theme-xs text-gray-500 dark:text-gray-400">
                  {v.text}
                </div>
              )}
            </div>
          </div>

          <div className="mt-auto flex flex-wrap items-center gap-x-3 gap-y-1 text-theme-xs text-gray-500 dark:text-gray-400">
            <span className="inline-flex items-center gap-1 font-medium text-gray-600 dark:text-gray-300">
              <ClockIcon className="size-3.5" />
              {fmtDuration(v.duration_seconds)}
            </span>
            {v.file_size > 0 && <span>{fmtSize(v.file_size)}</span>}
            {v.width > 0 && (
              <span>
                {v.width}×{v.height}
              </span>
            )}
            {v.date && <span className="ml-auto tabular-nums">{fmtDate(v.date)}</span>}
          </div>
        </Link>
      ))}
    </div>
  );
}
