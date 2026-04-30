import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, IndexStatus, TgStatus } from "../api/client";

export function Layout() {
  const nav = useNavigate();
  const tg = useQuery<TgStatus>({ queryKey: ["tg"], queryFn: () => api.get("/api/tg/status") });
  const idx = useQuery<IndexStatus>({
    queryKey: ["index"],
    queryFn: () => api.get("/api/index/status"),
    refetchInterval: (q) => (q.state.data?.status === "running" ? 2000 : false),
  });

  const onLogout = async () => {
    await api.post("/api/auth/logout");
    nav("/login");
  };

  const linkCls = ({ isActive }: { isActive: boolean }) =>
    "px-3 py-1 rounded " + (isActive ? "bg-slate-700" : "hover:bg-slate-800");

  return (
    <div className="min-h-screen flex flex-col">
      <header className="flex items-center gap-4 px-4 py-3 border-b border-slate-800">
        <Link to="/" className="text-lg font-semibold">📼 TG Video</Link>
        <NavLink to="/" end className={linkCls}>频道</NavLink>
        <NavLink to="/favorites" className={linkCls}>收藏</NavLink>
        <NavLink to="/search" className={linkCls}>搜索</NavLink>
        {tg.data && !tg.data.bound && <NavLink to="/tg/bind" className={linkCls}>绑定 TG</NavLink>}
        <div className="flex-1" />
        {idx.data?.status === "running" && (
          <span className="text-xs text-amber-300">
            索引中 {idx.data.channels_done}/{idx.data.channels_total} · {idx.data.videos_found} 视频
          </span>
        )}
        <button onClick={onLogout} className="text-sm text-slate-400 hover:text-slate-100">退出</button>
      </header>
      <main className="flex-1 overflow-auto"><Outlet /></main>
    </div>
  );
}
