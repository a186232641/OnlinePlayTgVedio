import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { api, Channel, DialogKind } from "../api/client";

const KIND_LABEL: Record<DialogKind, string> = {
  channel: "频道",
  megagroup: "群组",
  forum: "论坛",
  topic: "话题",
  group: "群",
  user: "私聊",
};

const KIND_COLOR: Record<DialogKind, string> = {
  channel: "bg-emerald-900/40 text-emerald-300",
  megagroup: "bg-sky-900/40 text-sky-300",
  forum: "bg-purple-900/40 text-purple-300",
  topic: "bg-purple-900/20 text-purple-200",
  group: "bg-sky-900/40 text-sky-300",
  user: "bg-slate-700 text-slate-300",
};

export function SessionChannels() {
  const { id } = useParams<{ id: string }>();
  const sessionId = Number(id);
  const qc = useQueryClient();
  const [filter, setFilter] = useState("");
  const [kindFilter, setKindFilter] = useState<DialogKind | "all">("all");

  const q = useQuery<{ channels: Channel[] }>({
    queryKey: ["channels", "session", sessionId],
    queryFn: () => api.get(`/api/channels/?session_id=${sessionId}`),
    refetchInterval: (qq) => {
      const list = qq.state.data?.channels ?? [];
      return list.some((c) => c.index_status === "running" || c.index_status === "queued") ? 3000 : false;
    },
  });

  const enable = useMutation({
    mutationFn: (cid: number) => api.post(`/api/channels/${cid}/index`),
    onSettled: () => qc.invalidateQueries({ queryKey: ["channels"] }),
  });
  const disable = useMutation({
    mutationFn: (cid: number) => api.del(`/api/channels/${cid}/index`),
    onSettled: () => qc.invalidateQueries({ queryKey: ["channels"] }),
  });

  const list = q.data?.channels ?? [];

  // group children under their parent (forum -> topics)
  const grouped = useMemo(() => {
    const byParent = new Map<number | null, Channel[]>();
    for (const c of list) {
      const k = c.parent_channel_id ?? null;
      if (!byParent.has(k)) byParent.set(k, []);
      byParent.get(k)!.push(c);
    }
    return byParent;
  }, [list]);

  const roots = (grouped.get(null) ?? []).filter((c) => {
    if (kindFilter !== "all" && c.dialog_kind !== kindFilter) return false;
    if (filter && !c.title.toLowerCase().includes(filter.toLowerCase())) return false;
    return true;
  });

  if (q.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;

  return (
    <div className="max-w-4xl mx-auto p-6 space-y-4">
      <div className="flex items-center gap-3">
        <Link to="/tg/accounts" className="text-sm text-slate-400 hover:text-slate-200">← 返回</Link>
        <h1 className="text-2xl font-semibold">选择要索引的频道</h1>
      </div>

      <div className="flex items-center gap-3">
        <input
          className="flex-1 px-3 py-1.5 bg-slate-800 rounded text-sm"
          placeholder="按标题过滤…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        <select
          className="px-3 py-1.5 bg-slate-800 rounded text-sm"
          value={kindFilter}
          onChange={(e) => setKindFilter(e.target.value as DialogKind | "all")}
        >
          <option value="all">全部</option>
          <option value="channel">频道</option>
          <option value="megagroup">群组</option>
          <option value="forum">论坛(含话题)</option>
          <option value="group">群</option>
          <option value="user">私聊</option>
        </select>
      </div>

      <div className="text-xs text-slate-500">
        勾选后,后台会按账号串行索引,FLOOD_WAIT 会自动等待。已索引的视频可以在"频道"页直接浏览/播放/收藏。
      </div>

      <div className="space-y-1">
        {roots.length === 0 && (
          <div className="text-slate-400 py-12 text-center">暂无频道,试试右上角的"重新发现"。</div>
        )}
        {roots.map((c) => (
          <ChannelRow
            key={c.id}
            c={c}
            children={grouped.get(c.id) ?? []}
            onEnable={enable.mutate}
            onDisable={disable.mutate}
          />
        ))}
      </div>
    </div>
  );
}

function ChannelRow({
  c, children, onEnable, onDisable,
}: {
  c: Channel;
  children: Channel[];
  onEnable: (id: number) => void;
  onDisable: (id: number) => void;
}) {
  const isForum = c.dialog_kind === "forum";
  const [expanded, setExpanded] = useState(isForum);

  const enabledChildCount = children.filter((t) => t.index_enabled).length;
  const totalChildCount = children.length;
  const allEnabled = totalChildCount > 0 && enabledChildCount === totalChildCount;
  const totalTopicVideos = isForum
    ? children.reduce((acc, t) => acc + (t.video_count ?? 0), 0)
    : c.video_count;

  return (
    <div className="rounded bg-slate-900 border border-slate-800">
      <div className="flex items-center gap-3 px-4 py-2.5">
        {isForum ? (
          <button
            onClick={() => setExpanded((v) => !v)}
            className="w-6 text-center text-slate-400 hover:text-slate-200"
            title={expanded ? "收起话题" : "展开话题"}
          >{expanded ? "▾" : "▸"}</button>
        ) : (
          <span className="w-6" />
        )}
        <span className={`px-1.5 py-0.5 rounded text-[10px] ${KIND_COLOR[c.dialog_kind]}`}>
          {KIND_LABEL[c.dialog_kind]}
        </span>
        <div className="flex-1 min-w-0">
          <div className="truncate">{c.title}</div>
          {c.username && <div className="text-xs text-slate-500">@{c.username}</div>}
          {isForum && totalChildCount > 0 && (
            <div className="text-xs text-slate-500 mt-0.5">
              已开启索引 {enabledChildCount}/{totalChildCount} 个话题
            </div>
          )}
        </div>
        <div className="text-xs text-slate-500 w-20 text-right">{totalTopicVideos} 视频</div>
        <StatusBadge c={c} />
        {isForum ? (
          totalChildCount === 0 ? (
            <span className="text-xs text-slate-500 px-3 py-1">无话题</span>
          ) : allEnabled ? (
            <button
              onClick={() => onDisable(c.id)}
              className="px-3 py-1 text-xs bg-slate-700 hover:bg-slate-600 rounded"
              title="关闭所有子话题的索引"
            >全部关闭</button>
          ) : (
            <button
              onClick={() => onEnable(c.id)}
              className="px-3 py-1 text-xs bg-emerald-700 hover:bg-emerald-600 rounded"
              title="开启所有子话题的索引"
            >{enabledChildCount > 0 ? "开启剩余" : "全部开启"}</button>
          )
        ) : c.index_enabled ? (
          <button
            onClick={() => onDisable(c.id)}
            className="px-3 py-1 text-xs bg-slate-700 hover:bg-slate-600 rounded"
          >关闭索引</button>
        ) : (
          <button
            onClick={() => onEnable(c.id)}
            className="px-3 py-1 text-xs bg-emerald-700 hover:bg-emerald-600 rounded"
          >开启索引</button>
        )}
      </div>
      {isForum && expanded && children.length > 0 && (
        <div className="border-t border-slate-800 pl-8">
          {children.map((t) => (
            <ChannelRow
              key={t.id}
              c={t}
              children={[]}
              onEnable={onEnable}
              onDisable={onDisable}
            />
          ))}
        </div>
      )}
      {isForum && expanded && children.length === 0 && (
        <div className="px-8 py-2 text-xs text-slate-500 border-t border-slate-800">
          这个论坛没有发现话题(可能需要"重新发现")。
        </div>
      )}
    </div>
  );
}

function StatusBadge({ c }: { c: Channel }) {
  if (c.index_status === "running")
    return <span className="text-xs text-amber-300 w-20 text-right">索引中…</span>;
  if (c.index_status === "queued")
    return <span className="text-xs text-slate-400 w-20 text-right">排队中</span>;
  if (c.index_status === "failed")
    return <span className="text-xs text-red-400 w-20 text-right" title={c.index_error}>失败</span>;
  return <span className="w-20" />;
}
