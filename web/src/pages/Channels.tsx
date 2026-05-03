import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, Channel, TgSession } from "../api/client";

// Browsing view: any channel that has imported videos shows up as a card.
// Forums and topics are filtered out by the backend.
export function Channels() {
  const sessions = useQuery<{ sessions: TgSession[] }>({
    queryKey: ["sessions"],
    queryFn: () => api.get("/api/tg/sessions/"),
  });
  const all = useQuery<{ channels: Channel[] }>({
    queryKey: ["channels", "all"],
    queryFn: () => api.get("/api/channels/"),
  });

  if (all.isLoading || sessions.isLoading)
    return <div className="p-6 text-slate-400">加载中…</div>;

  const sessList = sessions.data?.sessions ?? [];
  if (sessList.length === 0) {
    return (
      <div className="p-6 text-slate-400 space-y-2">
        <div>还没有绑定 TG 账号。</div>
        <Link to="/tg/bind" className="text-emerald-400 hover:text-emerald-300">前往绑定 →</Link>
      </div>
    );
  }

  const channels = all.data?.channels ?? [];
  const browsable = channels.filter((c) => c.video_count > 0);

  if (browsable.length === 0) {
    return (
      <div className="p-6 text-slate-400 space-y-2">
        <div>还没有导入任何视频。</div>
        <Link to="/tg/accounts" className="text-emerald-400 hover:text-emerald-300">去 TG 账号管理上传 JSON →</Link>
      </div>
    );
  }

  return (
    <div className="p-6 grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
      {browsable.map((c) => (
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
