import { useEffect, useRef, useState } from "react";
import { Link, NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { api, Me, TgSession } from "../api/client";
import { useTheme } from "../theme";
import { cx } from "./ui";
import {
  ChevronLeftIcon,
  CloseIcon,
  FilmIcon,
  GridIcon,
  LinkIcon,
  LogoutIcon,
  MenuIcon,
  MoonIcon,
  SearchIcon,
  StarIcon,
  SunIcon,
  UsersIcon,
} from "./icons";

const SIDEBAR_KEY = "tgv-sidebar-collapsed";

type NavItem = {
  to: string;
  label: string;
  icon: (p: { className?: string }) => JSX.Element;
  end?: boolean;
  badge?: string;
  show?: boolean;
};

// App shell: fixed sidebar (290px expanded / 90px collapsed with hover-peek)
// beside a sticky top header, per the workbench layout. Below `lg` the sidebar
// becomes a backdrop-covered drawer.
export function Layout() {
  const nav = useNavigate();
  const qc = useQueryClient();
  const location = useLocation();
  const { theme, toggle } = useTheme();

  const [drawerOpen, setDrawerOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(SIDEBAR_KEY) === "1";
    } catch {
      return false;
    }
  });
  const [peeking, setPeeking] = useState(false);

  useEffect(() => {
    try {
      localStorage.setItem(SIDEBAR_KEY, collapsed ? "1" : "0");
    } catch {
      /* storage disabled — collapse still works for this session */
    }
  }, [collapsed]);

  // Close the mobile drawer on navigation.
  useEffect(() => setDrawerOpen(false), [location.pathname]);

  // Same key + same null-on-failure shape as App's guard, so the two observers
  // don't fight over the cache entry.
  const me = useQuery<Me | null>({
    queryKey: ["me"],
    queryFn: async () => {
      try {
        return await api.get<Me>("/api/auth/me");
      } catch {
        return null;
      }
    },
  });

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
    qc.clear();
    nav("/login");
  };

  const items: NavItem[] = [
    { to: "/", label: "频道", icon: GridIcon, end: true },
    { to: "/favorites", label: "收藏", icon: StarIcon },
    { to: "/search", label: "搜索", icon: SearchIcon },
    { to: "/tg/accounts", label: "TG 账号", icon: UsersIcon, badge: String(list.length) },
    { to: "/tg/bind", label: "绑定 TG", icon: LinkIcon, show: noAccount },
  ];

  // Expanded when not collapsed, or while the pointer peeks at the rail.
  const expanded = !collapsed || peeking;

  return (
    <div className="min-h-screen">
      {/* backdrop — mobile drawer only */}
      {drawerOpen && (
        <div
          className="fixed inset-0 z-backdrop bg-gray-900/40 backdrop-blur-[2px] lg:hidden"
          onClick={() => setDrawerOpen(false)}
        />
      )}

      <aside
        onMouseEnter={() => collapsed && setPeeking(true)}
        onMouseLeave={() => setPeeking(false)}
        className={cx(
          "fixed inset-y-0 left-0 z-sidebar flex flex-col border-r border-gray-200 bg-white",
          "transition-[width,transform] duration-200 ease-out dark:border-gray-800 dark:bg-gray-dark",
          expanded ? "w-[290px]" : "w-[90px]",
          drawerOpen ? "translate-x-0" : "-translate-x-full lg:translate-x-0",
        )}
      >
        <div
          className={cx(
            "flex h-16 shrink-0 items-center gap-2 border-b border-gray-200 dark:border-gray-800",
            expanded ? "px-5" : "justify-center px-2",
          )}
        >
          <Link to="/" className="flex min-w-0 items-center gap-2.5">
            <span className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-brand-500 text-white">
              <FilmIcon className="size-5" />
            </span>
            {expanded && (
              <span className="truncate text-theme-xl font-semibold text-gray-800 dark:text-white/90">
                TG Video
              </span>
            )}
          </Link>
          <button
            className="btn-icon btn-ghost ml-auto lg:hidden"
            aria-label="关闭菜单"
            onClick={() => setDrawerOpen(false)}
          >
            <CloseIcon className="size-5" />
          </button>
        </div>

        <nav className="custom-scrollbar flex-1 overflow-y-auto p-4">
          {expanded && (
            <div className="mb-2 px-3 text-theme-xs font-medium uppercase text-gray-400 dark:text-gray-500">
              导航
            </div>
          )}
          <ul className="flex flex-col gap-1">
            {items
              .filter((i) => i.show !== false)
              .map((item) => (
                <li key={item.to}>
                  <NavLink
                    to={item.to}
                    end={item.end}
                    title={expanded ? undefined : item.label}
                    className={({ isActive }) =>
                      cx(
                        "menu-item",
                        isActive ? "menu-item-active" : "menu-item-inactive",
                        !expanded && "justify-center px-0",
                      )
                    }
                  >
                    <item.icon className="size-6 shrink-0" />
                    {expanded && (
                      <>
                        <span className="truncate">{item.label}</span>
                        {item.badge && (
                          <span className="badge badge-gray ml-auto">{item.badge}</span>
                        )}
                      </>
                    )}
                  </NavLink>
                </li>
              ))}
          </ul>
        </nav>

        <div className="shrink-0 border-t border-gray-200 p-4 dark:border-gray-800">
          <button
            onClick={() => setCollapsed((v) => !v)}
            className={cx(
              "menu-item menu-item-inactive hidden w-full lg:flex",
              !expanded && "justify-center px-0",
            )}
            title={collapsed ? "展开侧边栏" : "收起侧边栏"}
          >
            <ChevronLeftIcon
              className={cx("size-6 shrink-0 transition-transform", collapsed && "rotate-180")}
            />
            {expanded && <span>收起侧边栏</span>}
          </button>
        </div>
      </aside>

      <div
        className={cx(
          "flex min-h-screen flex-col transition-[margin] duration-200 ease-out",
          collapsed ? "lg:ml-[90px]" : "lg:ml-[290px]",
        )}
      >
        <header className="sticky top-0 z-header border-b border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-dark">
          <div className="flex h-16 items-center gap-3 px-4 lg:px-6">
            <button
              className="btn-icon btn-ghost -ml-2 lg:hidden"
              aria-label="菜单"
              aria-expanded={drawerOpen}
              onClick={() => setDrawerOpen(true)}
            >
              <MenuIcon className="size-5" />
            </button>

            <span className="text-theme-sm font-medium text-gray-500 lg:hidden dark:text-gray-400">
              TG Video
            </span>

            <div className="flex-1" />

            {anyDiscovering && (
              <span className="badge badge-warning">
                <span className="size-1.5 animate-pulse rounded-full bg-warning-500" />
                发现频道中…
              </span>
            )}

            <button
              onClick={toggle}
              className="btn-icon btn-ghost"
              aria-label={theme === "dark" ? "切换到浅色" : "切换到深色"}
              title={theme === "dark" ? "切换到浅色" : "切换到深色"}
            >
              {theme === "dark" ? <SunIcon className="size-5" /> : <MoonIcon className="size-5" />}
            </button>

            <UserMenu email={me.data?.email} onLogout={onLogout} />
          </div>
        </header>

        <main className="flex-1">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

// UserMenu — the header's account dropdown. Floating, so it earns a shadow.
function UserMenu({ email, onLogout }: { email?: string; onLogout: () => void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const initial = (email ?? "?").slice(0, 1).toUpperCase();

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-2 rounded-full p-0.5 transition-colors hover:bg-gray-100 dark:hover:bg-white/[0.06]"
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <span className="flex size-9 items-center justify-center rounded-full bg-brand-50 text-theme-sm font-semibold text-brand-500 dark:bg-brand-500/[0.15] dark:text-brand-400">
          {initial}
        </span>
        <span className="hidden max-w-[160px] truncate pr-2 text-theme-sm text-gray-700 md:inline dark:text-gray-300">
          {email ?? "账号"}
        </span>
      </button>

      {open && (
        <div className="panel-floating absolute right-0 mt-2 w-56 p-2">
          <div className="truncate px-3 py-2 text-theme-xs text-gray-500 dark:text-gray-400">
            {email ?? "已登录"}
          </div>
          <button
            onClick={() => {
              setOpen(false);
              onLogout();
            }}
            className="menu-item menu-item-inactive w-full"
          >
            <LogoutIcon className="size-5" />
            退出登录
          </button>
        </div>
      )}
    </div>
  );
}
