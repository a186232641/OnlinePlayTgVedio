import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, Channel, TgSession } from "../api/client";
import { ChevronRightIcon } from "../components/icons";
import { EmptyState, LoadingState, PageHeader } from "../components/ui";

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

  if (all.isLoading || sessions.isLoading) return <LoadingState />;

  const sessList = sessions.data?.sessions ?? [];
  const channels = all.data?.channels ?? [];
  const browsable = channels.filter((c) => c.video_count > 0);
  const totalVideos = browsable.reduce((n, c) => n + c.video_count, 0);

  return (
    <div className="p-4 md:p-6">
      <PageHeader
        title="我的频道"
        meta={
          browsable.length > 0
            ? `${browsable.length} 个频道 · ${totalVideos.toLocaleString()} 个视频`
            : undefined
        }
      />

      {sessList.length === 0 ? (
        <EmptyState
          title="还没有绑定 TG 账号"
          hint="绑定一个 Telegram 账号后,就能索引你已加入的频道里的视频。"
          action={
            <Link to="/tg/bind" className="btn btn-primary">
              前往绑定
            </Link>
          }
        />
      ) : browsable.length === 0 ? (
        <EmptyState
          title="还没有导入任何视频"
          hint="到 TG 账号管理里对频道执行「TG 同步」,或上传 Telegram Desktop 导出的 result.json。"
          action={
            <Link to="/tg/accounts" className="btn btn-primary">
              去 TG 账号管理
            </Link>
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3 3xl:grid-cols-4">
          {browsable.map((c) => (
            <Link
              key={c.id}
              to={`/channels/${c.id}`}
              className="card group flex items-center gap-3 p-5 transition-colors hover:border-brand-300 hover:bg-brand-25 dark:hover:border-brand-500/40 dark:hover:bg-brand-500/[0.06]"
            >
              <div className="min-w-0 flex-1">
                <div className="truncate font-medium text-gray-800 transition-colors group-hover:text-brand-600 dark:text-white/90 dark:group-hover:text-brand-400">
                  {c.title}
                </div>
                {c.username && (
                  <div className="truncate text-theme-xs text-gray-500 dark:text-gray-400">
                    @{c.username}
                  </div>
                )}
                <div className="mt-3">
                  <span className="badge badge-brand">{c.video_count.toLocaleString()} 视频</span>
                </div>
              </div>
              <ChevronRightIcon className="size-5 shrink-0 text-gray-300 transition-colors group-hover:text-brand-500 dark:text-gray-600" />
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
