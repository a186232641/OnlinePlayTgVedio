import { Link } from "react-router-dom";

import { Video } from "../api/client";

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
}: {
  videos: Video[];
  linkTo?: (v: Video) => string;
}) {
  if (videos.length === 0) return <div className="p-6 text-slate-400">暂无视频</div>;
  return (
    <div className="p-6 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-3">
      {videos.map((v) => (
        <Link
          key={v.id}
          to={linkTo ? linkTo(v) : `/videos/${v.id}`}
          className="group p-3 rounded bg-slate-900 border border-slate-800 hover:border-emerald-700 flex flex-col gap-2 min-h-[110px]"
        >
          <div className="text-sm font-medium leading-snug line-clamp-2 break-all group-hover:text-emerald-300">
            {v.file_name?.trim() || v.text?.trim() || <span className="text-slate-500">视频 #{v.id}</span>}
          </div>
          {v.file_name && v.text && (
            <div className="text-xs text-slate-400 line-clamp-2">{v.text}</div>
          )}
          <div className="mt-auto flex items-center gap-2 text-xs text-slate-500 flex-wrap">
            <span className="px-1.5 py-0.5 bg-slate-800 rounded">{fmtDuration(v.duration_seconds)}</span>
            {v.file_size > 0 && <span>{fmtSize(v.file_size)}</span>}
            {v.width > 0 && <span>{v.width}×{v.height}</span>}
            {v.date && <span className="ml-auto">{fmtDate(v.date)}</span>}
          </div>
        </Link>
      ))}
    </div>
  );
}
