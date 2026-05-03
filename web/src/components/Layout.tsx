import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, TgSession } from "../api/client";

export function Layout() {
  const nav = useNavigate();
  const sessions = useQuery<{ sessions: TgSession[] }>({
    queryKey: ["sessions"],
    queryFn: () => api.get("/api/tg/sessions/"),
    refetchInterval: (q) => {
      const list = q.state.data?.sessions ?? [];
      return list.some((s) => s.discover_status === "running") ? 2000 : false;
    },
  });

  const list = sessions.data?.sessions ?? [];
  const noAccount = !sessions.isLoading && list.length === 0;
  const anyDiscovering = list.some((s) => s.discover_status === "running");

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
        <NavLink to="/tg/accounts" className={linkCls}>TG 账号 ({list.length})</NavLink>
        {noAccount && <NavLink to="/tg/bind" className={linkCls}>绑定 TG</NavLink>}
        <div className="flex-1" />
        {anyDiscovering && <span className="text-xs text-amber-300">发现频道中…</span>}
        <button onClick={onLogout} className="text-sm text-slate-400 hover:text-slate-100">退出</button>
      </header>
      <main className="flex-1 overflow-auto"><Outlet /></main>
    </div>
  );
}
