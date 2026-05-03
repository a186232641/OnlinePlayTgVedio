import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";

import { api, Channel } from "../api/client";

interface ImportResp { ok: boolean; imported: number; skipped: number; total: number; }

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

export function SessionChannels() {
  const { id } = useParams<{ id: string }>();
  const sessionId = Number(id);
  const qc = useQueryClient();
  const [filter, setFilter] = useState("");

  const q = useQuery<{ channels: Channel[] }>({
    queryKey: ["channels", "session", sessionId],
    queryFn: () => api.get(`/api/channels/?session_id=${sessionId}`),
  });

  const importJson = useMutation({
    mutationFn: ({ cid, file }: { cid: number; file: File }) => uploadJsonImport(cid, file),
    onSettled: () => qc.invalidateQueries({ queryKey: ["channels"] }),
    onSuccess: (resp) => {
      alert(`导入完成: 写入 ${resp.imported} 条视频,跳过 ${resp.skipped} 条非视频消息(总 ${resp.total})。\n\n首次播放时会从 TG 现取 file_reference,稍慢一点。`);
    },
    onError: (err: Error) => alert(`导入失败: ${err.message}`),
  });

  const list = q.data?.channels ?? [];
  const visible = filter
    ? list.filter((c) => c.title.toLowerCase().includes(filter.toLowerCase()))
    : list;

  if (q.isLoading) return <div className="p-6 text-slate-400">加载中…</div>;

  return (
    <div className="max-w-4xl mx-auto p-6 space-y-4">
      <div className="flex items-center gap-3">
        <Link to="/tg/accounts" className="text-sm text-slate-400 hover:text-slate-200">← 返回</Link>
        <h1 className="text-2xl font-semibold">导入频道视频</h1>
      </div>

      <input
        className="w-full px-3 py-1.5 bg-slate-800 rounded text-sm"
        placeholder="按标题过滤…"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
      />

      <div className="text-xs text-slate-500">
        在 Telegram Desktop 选目标频道 → ⋯ → Export chat history → 选 JSON 格式 → 取消所有媒体勾选 → 导出。
        然后把生成的 result.json 在对应频道行点"导入 JSON"上传即可。
      </div>

      <div className="space-y-1">
        {visible.length === 0 && (
          <div className="text-slate-400 py-12 text-center">暂无频道,试试在 TG 账号页"重新发现"。</div>
        )}
        {visible.map((c) => (
          <ChannelRow
            key={c.id}
            c={c}
            onImport={(file) => importJson.mutate({ cid: c.id, file })}
            importing={importJson.isPending && importJson.variables?.cid === c.id}
          />
        ))}
      </div>
    </div>
  );
}

function ChannelRow({
  c, onImport, importing,
}: {
  c: Channel;
  onImport: (file: File) => void;
  importing: boolean;
}) {
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  return (
    <div className="rounded bg-slate-900 border border-slate-800 flex items-center gap-3 px-4 py-2.5">
      <div className="flex-1 min-w-0">
        <div className="truncate">{c.title}</div>
        {c.username && <div className="text-xs text-slate-500">@{c.username}</div>}
      </div>
      <div className="text-xs text-slate-500 w-20 text-right">{c.video_count} 视频</div>
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
      <button
        onClick={() => fileInputRef.current?.click()}
        disabled={importing}
        className="px-3 py-1 text-xs bg-sky-800 hover:bg-sky-700 disabled:opacity-50 rounded"
        title="上传 TG Desktop 导出的 result.json"
      >{importing ? "上传中…" : "导入 JSON"}</button>
    </div>
  );
}
