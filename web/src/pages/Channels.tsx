import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, Channel, TgSession } from "../api/client";

// Browsing view: only channels with index_enabled=true (i.e. ones the user
// asked us to crawl). Account management lives at /tg/accounts.
export function Channels() {
  const sessions = useQuery<{ sessions: TgSession[] }>({
    queryKey: ["sessions"],
    queryFn: () => api.get("/api/tg/sessions/"),
  });
  const channels = useQuery<{ channels: Channel[] }>({
    queryKey: ["channels", "enabled"],
    queryFn: () => api.get("/api/channels/?enabled=true"),
  });

  if (channels.isLoading || sessions.isLoading)
    return <div className="p-6 text-slate-400">加载中…</div>;

  const list = channels.data?.channels ?? [];
  const sessList = sessions.data?.sessions ?? [];

  if (sessList.length === 0) {
    return (
      <div className="p-6 text-slate-400 space-y-2">
        <div>还没有绑定 TG 账号。</div>
        <Link to="/tg/bind" className="text-emerald-400 hover:text-emerald-300">前往绑定 →</Link>
      </div>
    );
  }
  if (list.length === 0) {
    return (
      <div className="p-6 text-slate-400 space-y-2">
        <div>还没有频道开启索引。</div>
        <Link to="/tg/accounts" className="text-emerald-400 hover:text-emerald-300">去 TG 账号管理选择频道 →</Link>
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
          <div className="text-xs mt-2 text-slate-500 flex items-center gap-2">
            <span>{c.video_count} 视频</span>
            {c.index_status === "running" && <span className="text-amber-300">索引中…</span>}
            {c.index_status === "queued" && <span className="text-slate-400">排队</span>}
            {c.index_status === "failed" && <span className="text-red-400">失败</span>}
          </div>
        </Link>
      ))}
    </div>
  );
}
