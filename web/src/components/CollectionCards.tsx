import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FormEvent, useState } from "react";
import { Link } from "react-router-dom";

import { api, ApiError, Collection } from "../api/client";
import { dayLabel } from "../dates";
import { Cover } from "./Cover";
import { PlusIcon } from "./icons";
import { AlertStrip, EmptyState, LoadingState } from "./ui";

// CollectionCards is the "我的分组" view: a create box and one card per
// collection, most recently used first, each opening /collections/:id.
export function CollectionCards() {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const q = useQuery<{ items: Collection[] }>({
    queryKey: ["collections", "list"],
    queryFn: () => api.get("/api/collections/"),
  });
  const create = useMutation({
    mutationFn: (n: string) => api.post<Collection>("/api/collections/", { name: n }),
    onSuccess: () => {
      setName("");
      qc.invalidateQueries({ queryKey: ["collections"] });
    },
    onError: (e: Error) => alert(`新建分组失败: ${e.message}`),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const n = name.trim();
    if (n) create.mutate(n);
  };

  if (q.error) {
    return <AlertStrip title="加载分组失败">{(q.error as ApiError).message}</AlertStrip>;
  }
  const items = q.data?.items ?? [];

  return (
    <div className="space-y-4">
      <form onSubmit={submit} className="flex max-w-md gap-2">
        <input
          className="field min-w-0 flex-1"
          placeholder="新建分组,例如「最爱」…"
          maxLength={64}
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <button className="btn btn-primary" disabled={!name.trim() || create.isPending}>
          <PlusIcon className="size-4" />
          新建
        </button>
      </form>

      {q.isLoading ? (
        <LoadingState />
      ) : items.length === 0 ? (
        <EmptyState title="还没有分组 — 新建一个,然后在播放页或图片查看器里点「加入分组」" />
      ) : (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 2xl:grid-cols-5 3xl:grid-cols-6">
          {items.map((c) => (
            <Link
              key={c.id}
              to={`/collections/${c.id}`}
              className="card group flex flex-col overflow-hidden p-0 transition-colors hover:border-brand-300 dark:hover:border-brand-500/40"
            >
              <div className="relative aspect-[4/3] w-full overflow-hidden rounded-t-2xl">
                <Cover kind={c.cover_kind} src={c.cover_thumb_url} />
                <span className="absolute bottom-1.5 right-1.5 rounded-md bg-black/65 px-1.5 py-0.5 text-theme-xs tabular-nums text-white">
                  {(c.videos + c.photos).toLocaleString()} 条
                </span>
              </div>
              <div className="flex min-w-0 flex-1 flex-col gap-1 p-3">
                <div className="line-clamp-2 break-all text-theme-sm font-medium leading-snug text-gray-800 transition-colors group-hover:text-brand-600 dark:text-white/90 dark:group-hover:text-brand-400">
                  {c.name}
                </div>
                <div className="mt-auto flex flex-wrap items-center gap-x-2 text-theme-xs text-gray-500 dark:text-gray-400">
                  {c.videos > 0 && <span>{c.videos.toLocaleString()} 视频</span>}
                  {c.photos > 0 && <span>{c.photos.toLocaleString()} 图片</span>}
                  {c.videos + c.photos === 0 && <span>空</span>}
                  <span className="ml-auto">{dayLabel(c.last_added_at ?? c.created_at)}</span>
                </div>
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
