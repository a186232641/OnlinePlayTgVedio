import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, Channel, TgSession } from "../api/client";
import { ChevronRightIcon, TopicsIcon } from "../components/icons";
import { EmptyState, LoadingState, PageHeader } from "../components/ui";

// Browsing view: a channel shows up once it holds media. A forum group's own
// counters are always 0 (its content lives in its topics), so for a group what
// counts is the media summed across its topics — NOT merely having topics:
// discovery enumerates the topics of every forum the account has joined, so
// "has topics" is true for groups nobody has ever synced. Topic rows themselves
// are reached from the group card, not listed here.
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
  // For a forum group the media lives in its topics; for anything else, on the row.
  const videosOf = (c: Channel) => c.video_count + (c.is_forum ? c.topic_video_count : 0);
  const photosOf = (c: Channel) => c.photo_count + (c.is_forum ? c.topic_photo_count : 0);
  const browsable = channels.filter((c) => videosOf(c) > 0 || photosOf(c) > 0);
  const totalVideos = browsable.reduce((n, c) => n + videosOf(c), 0);
  const totalPhotos = browsable.reduce((n, c) => n + photosOf(c), 0);

  return (
    <div className="p-4 md:p-6">
      <PageHeader
        title="我的频道"
        meta={
          browsable.length > 0
            ? [
                `${browsable.length} 个频道/群组`,
                totalVideos > 0 && `${totalVideos.toLocaleString()} 个视频`,
                totalPhotos > 0 && `${totalPhotos.toLocaleString()} 张图片`,
              ]
                .filter(Boolean)
                .join(" · ")
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
          title="还没有导入任何内容"
          hint="到 TG 账号管理里对频道/群组执行「TG 同步」,或上传 Telegram Desktop 导出的 result.json。论坛群组会先拉话题列表,再逐个话题同步。"
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
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {c.is_forum && (
                    <span className="badge badge-gray inline-flex items-center gap-1">
                      <TopicsIcon className="size-3" />
                      {c.topic_count.toLocaleString()} 话题
                    </span>
                  )}
                  {videosOf(c) > 0 && (
                    <span className="badge badge-brand">{videosOf(c).toLocaleString()} 视频</span>
                  )}
                  {photosOf(c) > 0 && (
                    <span className="badge badge-gray">{photosOf(c).toLocaleString()} 图片</span>
                  )}
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
