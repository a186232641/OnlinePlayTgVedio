import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { api, TgSession } from "../api/client";

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

  if (q.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;
  const list = q.data?.sessions ?? [];

  return (
    <div className="max-w-3xl mx-auto p-6 space-y-4">
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-semibold">TG 账号管理</h1>
        <div className="flex-1" />
        <Link to="/tg/bind" className="px-3 py-1.5 bg-emerald-700 hover:bg-emerald-600 rounded text-sm">+ 添加账号</Link>
      </div>

      {list.length === 0 && (
        <div className="text-slate-400">还没有绑定任何 TG 账号。</div>
      )}

      <div className="space-y-2">
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

  return (
    <div className="p-4 rounded bg-slate-900 border border-slate-800 flex items-center gap-4">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          {editing ? (
            <>
              <input
                className="px-2 py-0.5 bg-slate-800 rounded text-sm"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                autoFocus
              />
              <button
                onClick={() => { onRename(label); setEditing(false); }}
                className="text-xs text-emerald-400"
              >保存</button>
              <button onClick={() => setEditing(false)} className="text-xs text-slate-400">取消</button>
            </>
          ) : (
            <>
              <div className="font-medium">{s.label || s.phone || `账号 #${s.id}`}</div>
              <button onClick={() => setEditing(true)} className="text-xs text-slate-400 hover:text-slate-200">改名</button>
            </>
          )}
        </div>
        <div className="text-xs text-slate-500 mt-0.5">
          {s.phone} · 状态: {s.status}
          {s.discover_status === "running" && <span className="ml-2 text-amber-300">发现频道中…</span>}
          {s.discover_status === "failed" && <span className="ml-2 text-red-400" title={s.discover_error}>发现失败</span>}
        </div>
      </div>
      <Link to={`/tg/accounts/${s.id}/channels`} className="text-sm text-emerald-400 hover:text-emerald-300">选频道 →</Link>
      <button onClick={onRefresh} className="text-sm text-slate-400 hover:text-slate-200">重新发现</button>
      <button onClick={onRemove} className="text-sm text-red-400 hover:text-red-300">解绑</button>
    </div>
  );
}
