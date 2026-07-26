import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { api, TgSession } from "../api/client";
import { ChevronRightIcon, PencilIcon, RefreshIcon, TrashIcon } from "../components/icons";
import { EmptyState, LoadingState, PageHeader } from "../components/ui";

// Session status → badge tone. Status is data state, so it earns a semantic
// colour; everything else on the row stays neutral.
const STATUS_BADGE: Record<TgSession["status"], { cls: string; label: string }> = {
  active: { cls: "badge-success", label: "已激活" },
  pending: { cls: "badge-warning", label: "待验证" },
  revoked: { cls: "badge-error", label: "已失效" },
};

export function TgAccounts() {
  const qc = useQueryClient();
  const q = useQuery<{ sessions: TgSession[] }>({
    queryKey: ["sessions"],
    queryFn: () => api.get("/api/tg/sessions/"),
    refetchInterval: (qq) => {
      const list = qq.state.data?.sessions ?? [];
      return list.some((s) => s.discover_status === "running") ? 2000 : false;
    },
  });

  const refresh = useMutation({
    mutationFn: (id: number) => api.post(`/api/tg/sessions/${id}/refresh`),
    onSettled: () => qc.invalidateQueries({ queryKey: ["sessions"] }),
  });

  const remove = useMutation({
    mutationFn: (id: number) => api.del(`/api/tg/sessions/${id}`),
    onSettled: () => qc.invalidateQueries({ queryKey: ["sessions"] }),
  });

  const rename = useMutation({
    mutationFn: ({ id, label }: { id: number; label: string }) =>
      api.patch(`/api/tg/sessions/${id}`, { label }),
    onSettled: () => qc.invalidateQueries({ queryKey: ["sessions"] }),
  });

  if (q.isLoading) return <LoadingState />;
  const list = q.data?.sessions ?? [];

  return (
    <div className="mx-auto w-full max-w-[900px] space-y-5 p-4 md:p-6">
      <PageHeader
        title="TG 账号管理"
        meta={`已绑定 ${list.length} 个 Telegram 账号`}
        actions={
          <Link to="/tg/bind" className="btn btn-primary">
            + 添加账号
          </Link>
        }
      />

      {list.length === 0 ? (
        <EmptyState
          title="还没有绑定任何 TG 账号"
          hint="绑定后会自动发现你已加入的频道与超级群,再对频道执行同步即可索引视频。"
          action={
            <Link to="/tg/bind" className="btn btn-primary">
              绑定 TG 账号
            </Link>
          }
        />
      ) : (
        <div className="flex flex-col gap-3">
          {list.map((s) => (
            <SessionCard
              key={s.id}
              s={s}
              onRefresh={() => refresh.mutate(s.id)}
              onRemove={() => {
                if (confirm(`解绑 ${s.phone || s.id}? 已索引的视频和收藏会保留,但无法继续播放和增量索引。`)) {
                  remove.mutate(s.id);
                }
              }}
              onRename={(label) => rename.mutate({ id: s.id, label })}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function SessionCard({
  s, onRefresh, onRemove, onRename,
}: {
  s: TgSession;
  onRefresh: () => void;
  onRemove: () => void;
  onRename: (label: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [label, setLabel] = useState(s.label ?? "");
  const status = STATUS_BADGE[s.status];

  return (
    <div className="card flex flex-wrap items-center gap-x-4 gap-y-3 p-4">
      <div className="min-w-[200px] flex-1">
        <div className="flex flex-wrap items-center gap-2">
          {editing ? (
            <>
              <input
                className="field field-sm w-48"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                autoFocus
              />
              <button
                onClick={() => { onRename(label); setEditing(false); }}
                className="btn btn-primary btn-sm"
              >保存</button>
              <button onClick={() => setEditing(false)} className="btn btn-outline btn-sm">
                取消
              </button>
            </>
          ) : (
            <>
              <span className="font-medium text-gray-800 dark:text-white/90">
                {s.label || s.phone || `账号 #${s.id}`}
              </span>
              <button
                onClick={() => setEditing(true)}
                className="btn-icon btn-ghost size-7"
                title="改名"
                aria-label="改名"
              >
                <PencilIcon className="size-4" />
              </button>
              <span className={"badge " + status.cls}>{status.label}</span>
              {s.discover_status === "running" && (
                <span className="badge badge-warning">
                  <span className="size-1.5 animate-pulse rounded-full bg-warning-500" />
                  发现频道中…
                </span>
              )}
              {s.discover_status === "failed" && (
                <span className="badge badge-error" title={s.discover_error}>
                  发现失败
                </span>
              )}
            </>
          )}
        </div>
        {s.phone && (
          <div className="mt-1 text-theme-xs text-gray-500 dark:text-gray-400">{s.phone}</div>
        )}
      </div>

      <div className="ml-auto flex flex-wrap items-center gap-2">
        <button onClick={onRefresh} className="btn btn-outline btn-sm" title="重新拉取该账号已加入的频道列表">
          <RefreshIcon className="size-4" />
          重新发现
        </button>
        <button onClick={onRemove} className="btn btn-danger btn-sm">
          <TrashIcon className="size-4" />
          解绑
        </button>
        <Link to={`/tg/accounts/${s.id}/channels`} className="btn btn-primary btn-sm">
          选频道
          <ChevronRightIcon className="size-4" />
        </Link>
      </div>
    </div>
  );
}
