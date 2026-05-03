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

// Pure-text video card. We used to render thumbnails synced from Telegram,
// but the simplified app reads everything from JSON imports which don't
// include thumbnail bytes — so the card is caption-led with metadata chips.
export function VideoGrid({ videos }: { videos: Video[] }) {
  if (videos.length === 0) return <div className="p-6 text-slate-400">暂无视频</div>;
  return (
    <div className="p-6 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-3">
      {videos.map((v) => (
        <Link
          key={v.id}
          to={`/videos/${v.id}`}
          className="group p-3 rounded bg-slate-900 border border-slate-800 hover:border-emerald-700 flex flex-col gap-2 min-h-[100px]"
        >
          <div className="text-sm leading-snug line-clamp-3 group-hover:text-emerald-300">
            {v.caption?.trim() || <span className="text-slate-500">视频 #{v.id}</span>}
          </div>
          <div className="mt-auto flex items-center gap-2 text-xs text-slate-500 flex-wrap">
            <span className="px-1.5 py-0.5 bg-slate-800 rounded">{fmtDuration(v.duration_sec)}</span>
            {v.size_bytes > 0 && <span>{fmtSize(v.size_bytes)}</span>}
            {v.width > 0 && <span>{v.width}×{v.height}</span>}
            {v.sent_at && <span className="ml-auto">{fmtDate(v.sent_at)}</span>}
          </div>
        </Link>
      ))}
    </div>
  );
}
