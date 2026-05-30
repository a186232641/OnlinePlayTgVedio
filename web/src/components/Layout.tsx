import { useState } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, TgSession } from "../api/client";

export function Layout() {
  const nav = useNavigate();
  const [menuOpen, setMenuOpen] = useState(false);

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

  const links: { to: string; label: string; end?: boolean; show?: boolean }[] = [
    { to: "/", label: "频道", end: true },
    { to: "/favorites", label: "收藏" },
    { to: "/search", label: "搜索" },
    { to: "/tg/accounts", label: `TG 账号 (${list.length})` },
    { to: "/tg/bind", label: "绑定 TG", show: noAccount },
  ];

  const linkCls = ({ isActive }: { isActive: boolean }) =>
    "px-3 py-1.5 rounded-lg text-sm font-medium transition-colors " +
    (isActive
      ? "bg-emerald-600 text-white shadow-sm shadow-emerald-900/40"
      : "text-slate-300 hover:bg-slate-800 hover:text-white");

  const navLinks = (onClick?: () => void) =>
    links
      .filter((l) => l.show !== false)
      .map((l) => (
        <NavLink key={l.to} to={l.to} end={l.end} className={linkCls} onClick={onClick}>
          {l.label}
        </NavLink>
      ));

  return (
    <div className="min-h-screen flex flex-col">
      <header className="sticky top-0 z-30 border-b border-slate-800 bg-slate-950/80 backdrop-blur supports-[backdrop-filter]:bg-slate-950/60">
        <div className="flex items-center gap-3 px-4 py-2.5">
          <Link
            to="/"
            className="flex items-center gap-2 text-lg font-semibold tracking-tight shrink-0"
            onClick={() => setMenuOpen(false)}
          >
            <span className="text-xl">📼</span>
            <span className="bg-gradient-to-r from-emerald-300 to-sky-300 bg-clip-text text-transparent">
              TG Video
            </span>
          </Link>

          {/* desktop nav */}
          <nav className="hidden md:flex items-center gap-1.5">{navLinks()}</nav>

          <div className="flex-1" />

          {anyDiscovering && (
            <span className="hidden sm:flex items-center gap-1.5 text-xs text-amber-300">
              <span className="w-1.5 h-1.5 rounded-full bg-amber-400 animate-pulse" />
              发现频道中…
            </span>
          )}

          <button
            onClick={onLogout}
            className="hidden md:inline text-sm text-slate-400 hover:text-slate-100 transition-colors"
          >
            退出
          </button>

          {/* mobile hamburger */}
          <button
            className="md:hidden p-2 -mr-2 rounded-lg text-slate-300 hover:bg-slate-800"
            aria-label="菜单"
            aria-expanded={menuOpen}
            onClick={() => setMenuOpen((v) => !v)}
          >
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
              {menuOpen ? <path d="M6 6l12 12M18 6L6 18" /> : <><path d="M4 7h16" /><path d="M4 12h16" /><path d="M4 17h16" /></>}
            </svg>
          </button>
        </div>

        {/* mobile dropdown */}
        {menuOpen && (
          <nav className="md:hidden flex flex-col gap-1 px-3 pb-3 border-t border-slate-800/70">
            <div className="flex flex-col gap-1 pt-2">{navLinks(() => setMenuOpen(false))}</div>
            {anyDiscovering && (
              <span className="px-3 py-1 text-xs text-amber-300">发现频道中…</span>
            )}
            <button
              onClick={() => { setMenuOpen(false); onLogout(); }}
              className="mt-1 px-3 py-1.5 text-left text-sm text-slate-400 hover:bg-slate-800 hover:text-slate-100 rounded-lg"
            >
              退出
            </button>
          </nav>
        )}
      </header>

      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}
