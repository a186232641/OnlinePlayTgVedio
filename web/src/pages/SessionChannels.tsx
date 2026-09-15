import { Link, useParams } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useRef, useState } from "react";

import { api, Channel, SyncState } from "../api/client";
import { LIST_PAGE_SIZE, useDebounced, usePagedList, useSyncStatuses } from "../api/paged";
import { ChevronLeftIcon, RefreshIcon, SearchIcon, TrashIcon, UploadIcon } from "../components/icons";
import { AlertStrip, LoadingState, MoreFooter, PageHeader, Toggle } from "../components/ui";

interface ImportResp {
  ok: boolean;
  imported: number;
  photos?: number;
  skipped: number;
  total: number;
  skip_by?: Record<string, number>;
}

interface ChannelsResp { channels: Channel[]; has_more?: boolean; total?: number }

async function uploadJsonImport(channelId: number, file: File): Promise<ImportResp> {
  const fd = new FormData();
  fd.append("file", file);
  const res = await fetch(`/api/channels/${channelId}/import`, {
    method: "POST",
    credentials: "include",
    body: fd,
  });
  const payload = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(payload?.message ?? `上传失败 (${res.status})`);
  }
  return payload as ImportResp;
}

function formatImportResult(r: ImportResp): string {
  let s = `导入完成: 写入 ${r.imported} 条视频、${r.photos ?? 0} 张图片\n总消息数 ${r.total},跳过 ${r.skipped} 条无媒体消息`;
  if (r.skip_by && Object.keys(r.skip_by).length > 0) {
    const top = Object.entries(r.skip_by)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 8)
      .map(([k, v]) => `  ${k || "(空)"}: ${v}`)
      .join("\n");
    s += `\n\n跳过类型分布(前 8):\n${top}`;
  }
  s += `\n\n首次播放/查看时会从 TG 现取 file_reference,稍慢一点。`;
  return s;
}

export function SessionChannels() {
  const { id } = useParams<{ id: string }>();
  const sessionId = Number(id);
  const qc = useQueryClient();
  const [filter, setFilter] = useState("");

  // Server-side search + paging: an account can sit in thousands of channels,
  // and this page used to render all of them (plus a status request per row)
  // before showing anything.
  const debounced = useDebounced(filter.trim());
  const { query: q, items: list, total } = usePagedList<Channel, ChannelsResp>({
    key: ["channels", "session", sessionId, debounced],
    path: `/api/channels/`,
    params: new URLSearchParams({ session_id: String(sessionId), ...(debounced ? { q: debounced } : {}) }),
    pick: (r) => r.channels,
    keyOf: (c) => c.id,
  });
  const statusIds = useMemo(() => list.map((c) => c.id), [list]);
  const { states } = useSyncStatuses(statusIds, () =>
    qc.invalidateQueries({ queryKey: ["channels", "session", sessionId] }),
  );

  const importJson = useMutation({
    mutationFn: ({ cid, file }: { cid: number; file: File }) => uploadJsonImport(cid, file),
    onSettled: () => qc.invalidateQueries({ queryKey: ["channels"] }),
    onSuccess: (resp) => alert(formatImportResult(resp)),
    onError: (err: Error) => alert(`导入失败: ${err.message}`),
  });
  const clearChannel = useMutation({
    mutationFn: async (cid: number) => {
      const res = await fetch(`/api/channels/${cid}/videos`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok) throw new Error(`清空失败 (${res.status})`);
      return res.json();
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["channels"] }),
    onSuccess: (resp: { deleted: number; videos: number; photos: number }) =>
      alert(`已清空 ${resp.deleted} 条 (视频 ${resp.videos} · 图片 ${resp.photos})`),
    onError: (err: Error) => alert(`清空失败: ${err.message}`),
  });
  const syncStart = useMutation({
    mutationFn: (cid: number) => api.post<SyncState>(`/api/channels/${cid}/sync`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sync-statuses"] }),
    onError: (err: Error) => alert(`同步启动失败: ${err.message}`),
  });
  // Re-arms the full-history walk. Needed to pick up media kinds that a channel
  // was already "done" for — images, for a channel synced before image support.
  const backfill = useMutation({
    mutationFn: (cid: number) => api.post<SyncState>(`/api/channels/${cid}/backfill`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sync-statuses"] }),
    onError: (err: Error) => alert(`回填启动失败: ${err.message}`),
  });

  if (q.isLoading && !debounced) return <LoadingState />;

  return (
    <div className="mx-auto w-full max-w-[1100px] space-y-5 p-4 md:p-6">
      <Link
        to="/tg/accounts"
        className="inline-flex items-center gap-1 text-theme-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
      >
        <ChevronLeftIcon className="size-4" />
        返回 TG 账号
      </Link>

      <PageHeader title="导入频道视频" meta={`该账号下发现 ${(total ?? list.length).toLocaleString()} 个频道`} />

      <AlertStrip tone="info" title="两种入库方式">
        <div className="space-y-1">
          <div>
            <span className="font-medium">TG 同步</span> — 后端用你的 TG session 直接拉消息历史(增量 +
            回填,可中断续传),推荐。
          </div>
          <div>
            <span className="font-medium">导入 JSON</span> — Telegram Desktop 选频道 → ⋯ → Export chat
            history → JSON 格式 → 取消所有媒体勾选 → 导出,再把 result.json 上传到对应频道行。
          </div>
        </div>
      </AlertStrip>

      <div className="relative">
        <SearchIcon className="pointer-events-none absolute left-3.5 top-1/2 size-5 -translate-y-1/2 text-gray-400" />
        <input
          className="field pl-11"
          placeholder="搜索频道标题…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
      </div>

      <div className="card" >
        <div className="hidden items-center gap-4 rounded-t-2xl border-b border-gray-200 bg-gray-50 px-5 py-3 text-theme-xs font-medium text-gray-500 lg:flex dark:border-gray-800 dark:bg-white/[0.02] dark:text-gray-400">
          <span className="flex-1">频道</span>
          <span className="w-24 text-right">视频 / 图片</span>
          <span className="w-20 text-center">自动同步</span>
          <span className="w-[360px] text-right">操作</span>
        </div>

        {q.isLoading ? (
          <LoadingState label="搜索中…" />
        ) : list.length === 0 ? (
          <div className="px-5 py-12 text-center text-theme-sm text-gray-500 dark:text-gray-400">
            {debounced ? "没有匹配的频道。" : "暂无频道,试试在 TG 账号页「重新发现」。"}
          </div>
        ) : (
          <div className="divide-y divide-gray-200 dark:divide-gray-800">
            {list.map((c) => (
              <ChannelRow
                key={c.id}
                c={c}
                sync={states[c.id]}
                onImport={(file) => importJson.mutate({ cid: c.id, file })}
                onClear={() => {
                  if (confirm(`清空 ${c.title} 的所有视频和图片(${c.video_count + c.photo_count} 条)?\n\n收藏会一并删除。常用于"重置后重新导入"。`)) {
                    clearChannel.mutate(c.id);
                  }
                }}
                onSync={() => syncStart.mutate(c.id)}
                onBackfill={() => {
                  if (
                    confirm(
                      `重新回填 ${c.title}?\n\n会重置"历史已走完"标记,下次同步从最早的消息重新走一遍,用来补齐旧的图片(已入库的不会重复写)。`,
                    )
                  ) {
                    backfill.mutate(c.id);
                  }
                }}
                importing={importJson.isPending && importJson.variables?.cid === c.id}
                clearing={clearChannel.isPending && clearChannel.variables === c.id}
              />
            ))}
          </div>
        )}
      </div>

      <MoreFooter
        hasNextPage={!!q.hasNextPage}
        isFetchingNextPage={q.isFetchingNextPage}
        fetchNextPage={q.fetchNextPage}
        doneLabel="已加载全部频道"
        loaded={list.length}
        pageSize={LIST_PAGE_SIZE}
      />
    </div>
  );
}

function ChannelRow({
  c, sync, onImport, onClear, onSync, onBackfill, importing, clearing,
}: {
  c: Channel;
  sync?: SyncState;
  onImport: (file: File) => void;
  onClear: () => void;
  onSync: () => void;
  onBackfill: () => void;
  importing: boolean;
  clearing: boolean;
}) {
  const qc = useQueryClient();
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  // Live state comes from the page's single batched poll (useSyncStatuses),
  // not a query per row.
  const isSyncing = !!sync?.running;
  const lastError = sync?.last_error;
  const note = sync?.note;

  const autoSync = useMutation({
    mutationFn: (val: boolean) => api.patch(`/api/channels/${c.id}`, { auto_sync: val }),
    onSettled: () => qc.invalidateQueries({ queryKey: ["channels"] }),
    onError: (e: Error) => alert(`切换自动同步失败: ${e.message}`),
  });

  const busy = importing || clearing || isSyncing;

  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-3 px-5 py-3.5">
      <div className="min-w-[220px] flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate font-medium text-gray-800 dark:text-white/90">{c.title}</span>
          {c.is_forum && (
            <span className="badge badge-gray shrink-0" title="论坛群组:同步会先拉话题列表,再逐个话题同步">
              话题群组 {c.topic_count > 0 ? c.topic_count : ""}
            </span>
          )}
        </div>
        {c.username && (
          <div className="truncate text-theme-xs text-gray-500 dark:text-gray-400">
            @{c.username}
          </div>
        )}

        {isSyncing && (
          <div className="mt-1 text-theme-xs text-warning-600 dark:text-warning-400">
            {sync?.phase && sync.phase !== "syncing" && `${sync.phase} · `}
            正在从 TG 同步:已遍历 {sync?.walked ?? 0} · 视频 {sync?.videos ?? 0} · 图片{" "}
            {sync?.photos ?? 0} · 跳过 {sync?.skipped ?? 0}
            <span className="text-gray-400 dark:text-gray-500">
              {" "}(边抓边写,首次大频道可能需多轮,中断会自动续传)
            </span>
          </div>
        )}
        {!isSyncing && lastError && (
          <div
            className="mt-1 truncate text-theme-xs text-error-600 dark:text-error-400"
            title={lastError}
          >
            上次同步失败: {lastError}
          </div>
        )}
        {!isSyncing && !lastError && note && (
          <div
            className="mt-1 truncate text-theme-xs text-blue-light-600 dark:text-blue-light-400"
            title={note}
          >
            {note}
          </div>
        )}
      </div>

      <div className="w-24 text-right text-theme-sm tabular-nums text-gray-600 dark:text-gray-300">
        {c.video_count.toLocaleString()}
        {c.photo_count > 0 && (
          <span className="block text-theme-xs text-gray-400">+{c.photo_count.toLocaleString()} 图</span>
        )}
      </div>

      <div className="flex w-20 justify-center">
        <Toggle
          size="sm"
          checked={c.auto_sync}
          disabled={autoSync.isPending}
          onChange={(v) => autoSync.mutate(v)}
          title="是否纳入后台定时自动同步(手动「TG 同步」不受此开关影响)"
        />
      </div>

      <input
        ref={fileInputRef}
        type="file"
        accept="application/json,.json"
        className="hidden"
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) onImport(f);
          e.target.value = "";
        }}
      />

      <div className="flex w-full flex-wrap items-center justify-end gap-2 lg:w-[360px]">
        <button
          onClick={onSync}
          disabled={busy}
          className="btn btn-primary btn-sm"
          title="后端用你的 TG session 直接拉这个频道的消息历史(增量,只取没见过的新消息)"
        >
          <RefreshIcon className="size-4" />
          {isSyncing ? "同步中…" : "TG 同步"}
        </button>
        <button
          onClick={() => fileInputRef.current?.click()}
          disabled={busy}
          className="btn btn-outline btn-sm"
          title="上传 TG Desktop 导出的 result.json"
        >
          <UploadIcon className="size-4" />
          {importing ? "上传中…" : "导入 JSON"}
        </button>
        {(c.video_count > 0 || c.photo_count > 0) && (
          <button
            onClick={onBackfill}
            disabled={busy}
            className="btn btn-outline btn-sm"
            title="重置「历史已走完」标记并重新同步 — 用来补齐旧消息里的图片"
          >
            <RefreshIcon className="size-4" />
            重新回填
          </button>
        )}
        {(c.video_count > 0 || c.photo_count > 0) && (
          <button
            onClick={onClear}
            disabled={busy}
            className="btn btn-danger btn-sm"
            title="清空该频道所有视频和图片(用于重新导入)"
          >
            <TrashIcon className="size-4" />
            {clearing ? "清空中…" : "清空"}
          </button>
        )}
      </div>
    </div>
  );
}
