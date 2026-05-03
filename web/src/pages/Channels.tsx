import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, Channel, TgSession } from "../api/client";

// Browsing view. Two kinds of cards:
//   - playable: regular channels/megagroups/topics with index_enabled=true
//   - forum folders: dialog_kind=forum (entry point to its topic list)
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
  const enabled = channels.filter((c) => c.index_enabled && c.dialog_kind !== "forum");
  const forums = channels.filter((c) => c.dialog_kind === "forum");

  if (enabled.length === 0 && forums.length === 0) {
    return (
      <div className="p-6 text-slate-400 space-y-2">
        <div>还没有可浏览的频道。</div>
        <Link to="/tg/accounts" className="text-emerald-400 hover:text-emerald-300">去 TG 账号管理 →</Link>
      </div>
    );
  }

  return (
    <div className="p-6 space-y-8">
      {forums.length > 0 && (
        <section>
          <h2 className="text-sm uppercase text-slate-500 tracking-wide mb-3">论坛 (点入查看话题)</h2>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
            {forums.map((f) => {
              const topicCount = channels.filter((c) => c.parent_channel_id === f.id).length;
              return (
                <Link
                  key={f.id}
                  to={`/channels/${f.id}`}
                  className="p-4 rounded bg-slate-900 border border-slate-800 hover:border-purple-700 flex items-center gap-3"
                >
                  <span className="text-2xl">📁</span>
                  <div className="min-w-0">
                    <div className="font-medium truncate">{f.title}</div>
                    <div className="text-xs text-slate-500">{topicCount} 个话题</div>
                  </div>
                </Link>
              );
            })}
          </div>
        </section>
      )}

      {enabled.length > 0 && (
        <section>
          <h2 className="text-sm uppercase text-slate-500 tracking-wide mb-3">已索引</h2>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
            {enabled.map((c) => (
              <Link key={c.id} to={`/channels/${c.id}`}
                    className="p-4 rounded bg-slate-900 border border-slate-800 hover:border-emerald-700">
                <div className="font-medium truncate">{c.title}</div>
                {c.username && <div className="text-xs text-slate-400">@{c.username}</div>}
                <div className="text-xs mt-2 text-slate-500 flex items-center gap-2">
                  <span>{c.video_count} 视频</span>
                  {c.dialog_kind === "topic" && <span className="text-purple-300">话题</span>}
                  {c.index_status === "running" && <span className="text-amber-300">索引中…</span>}
                </div>
              </Link>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}
