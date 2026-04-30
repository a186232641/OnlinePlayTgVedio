import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, Channel } from "../api/client";

export function Channels() {
  const q = useQuery<{ channels: Channel[] }>({
    queryKey: ["channels"],
    queryFn: () => api.get("/api/channels/"),
  });

  if (q.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;
  if (q.error) return <div className="p-6 text-red-400">加载失败</div>;
  const list = q.data?.channels ?? [];
  if (list.length === 0) {
    return (
      <div className="p-6 text-slate-400">
        还没有索引到任何频道。请确认 TG 已绑定,索引正在后台运行。
      </div>
    );
  }
  return (
    <div className="p-6 grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
      {list.map((c) => (
        <Link key={c.id} to={`/channels/${c.id}`}
              className="p-4 rounded bg-slate-900 border border-slate-800 hover:border-emerald-700">
          <div className="font-medium truncate">{c.title}</div>
          {c.username && <div className="text-xs text-slate-400">@{c.username}</div>}
          <div className="text-xs mt-2 text-slate-500">{c.video_count} 视频</div>
        </Link>
      ))}
    </div>
  );
}
