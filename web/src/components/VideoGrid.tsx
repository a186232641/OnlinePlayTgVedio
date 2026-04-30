import { Link } from "react-router-dom";

import { Video } from "../api/client";

function fmtDuration(s: number) {
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h) return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return `${m}:${String(sec).padStart(2, "0")}`;
}

export function VideoGrid({ videos }: { videos: Video[] }) {
  if (videos.length === 0) return <div className="p-6 text-slate-400">暂无视频</div>;
  return (
    <div className="p-6 grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4">
      {videos.map((v) => (
        <Link key={v.id} to={`/videos/${v.id}`}
              className="rounded overflow-hidden bg-slate-900 border border-slate-800 hover:border-emerald-700 group">
          <div className="aspect-video bg-slate-800 relative">
            {v.thumb_url ? (
              <img src={v.thumb_url} className="w-full h-full object-cover"
                   alt={v.caption.slice(0, 60)} loading="lazy" />
            ) : (
              <div className="w-full h-full flex items-center justify-center text-slate-600">无缩略图</div>
            )}
            <div className="absolute bottom-1 right-1 bg-black/70 text-xs px-1 rounded">
              {fmtDuration(v.duration_sec)}
            </div>
          </div>
          <div className="p-2 text-sm line-clamp-2 group-hover:text-emerald-300">
            {v.caption || <span className="text-slate-500">无说明</span>}
          </div>
        </Link>
      ))}
    </div>
  );
}
