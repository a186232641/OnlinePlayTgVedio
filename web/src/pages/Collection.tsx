import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";

import { api, ApiError, Collection as CollectionT, MediaItem } from "../api/client";
import { MEDIA_PAGE_SIZE, normalizeKind, useMediaPages } from "../api/media";
import { KindTabs, MediaBrowser } from "../components/MediaBrowser";
import { COLLECTION_SORT_OPTIONS, FAV_DEFAULT_SORT, SortSelect, normalizeSort } from "../components/SortSelect";
import { ChevronLeftIcon, PencilIcon, TrashIcon } from "../components/icons";
import { AlertStrip, LoadingState, MoreFooter, PageHeader } from "../components/ui";
import { groupFor } from "../dates";

// Collection is one of the user's own groups: its videos and images, newest
// added first by default, from any mix of channels, topics and streamers.
// Items are added/removed with the "加入分组" picker in the player and the
// image viewer. ?kind= and ?order= live in the URL so "返回分组" restores them.
export function Collection() {
  const { id } = useParams();
  const nav = useNavigate();
  const qc = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const kind = normalizeKind(searchParams.get("kind"));
  const order = normalizeSort(searchParams.get("order"), COLLECTION_SORT_OPTIONS, FAV_DEFAULT_SORT);

  const meta = useQuery<CollectionT>({
    queryKey: ["collections", "one", id],
    queryFn: () => api.get(`/api/collections/${id}`),
  });

  const { query: q, items, sources } = useMediaPages(
    ["collections", "media", id, kind, order],
    () => {
      const p = new URLSearchParams({ order });
      if (kind) p.set("kind", kind);
      return p;
    },
    { path: `/api/collections/${id}/media` },
  );
  const groupBy = useMemo(() => groupFor(order), [order]);

  const patch = (next: { kind?: string; order?: string }) => {
    const p = new URLSearchParams(searchParams);
    for (const [k, v] of Object.entries(next)) {
      if (v && !(k === "order" && v === FAV_DEFAULT_SORT)) p.set(k, v);
      else p.delete(k);
    }
    setSearchParams(p);
  };

  const rename = useMutation({
    mutationFn: (name: string) => api.patch(`/api/collections/${id}`, { name }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["collections"] }),
    onError: (e: Error) => alert(`重命名失败: ${e.message}`),
  });
  const remove = useMutation({
    mutationFn: () => api.del(`/api/collections/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["collections"] });
      nav("/favorites?group=collection");
    },
    onError: (e: Error) => alert(`删除失败: ${e.message}`),
  });

  // Videos carry the collection into the player so prev/next walk this list.
  const linkTo = (m: MediaItem) => {
    const p = new URLSearchParams({ coll: String(id), order });
    if (kind) p.set("kind", kind);
    return `/videos/${m.id}?${p}`;
  };

  if (meta.error) {
    const err = meta.error as ApiError;
    return (
      <div className="p-4 md:p-6">
        <AlertStrip title={err.status === 404 ? "分组不存在" : "加载分组失败"}>{err.message}</AlertStrip>
      </div>
    );
  }
  if (meta.isLoading || !meta.data) return <LoadingState />;
  const c = meta.data;

  return (
    <div className="space-y-5 p-4 md:p-6">
      <Link
        to="/favorites?group=collection"
        className="inline-flex items-center gap-1 text-theme-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
      >
        <ChevronLeftIcon className="size-4" />
        我的分组
      </Link>

      <PageHeader
        title={c.name}
        meta={
          <>
            {c.videos.toLocaleString()} 个视频 · {c.photos.toLocaleString()} 张图片 ·
            在播放页或图片查看器里点「加入分组」可加入/移出
          </>
        }
        actions={
          <>
            <button
              type="button"
              className="btn btn-outline btn-sm"
              disabled={rename.isPending}
              onClick={() => {
                const n = window.prompt("分组名称", c.name)?.trim();
                if (n && n !== c.name) rename.mutate(n);
              }}
            >
              <PencilIcon className="size-4" />
              重命名
            </button>
            <button
              type="button"
              className="btn btn-danger btn-sm"
              disabled={remove.isPending}
              onClick={() => {
                if (window.confirm(`删除分组「${c.name}」?分组里的视频和图片本身不受影响。`)) remove.mutate();
              }}
            >
              <TrashIcon className="size-4" />
              删除
            </button>
          </>
        }
      />

      <div className="flex flex-wrap items-center gap-2">
        <KindTabs value={kind} onChange={(k) => patch({ kind: k })} counts={{ videos: c.videos, photos: c.photos }} />
        <SortSelect
          value={order}
          onChange={(o) => patch({ order: o })}
          options={COLLECTION_SORT_OPTIONS}
          className="field field-select ml-auto w-auto"
        />
      </div>

      {q.error ? (
        <AlertStrip title="加载分组内容失败">{(q.error as ApiError).message}</AlertStrip>
      ) : (
        <MediaBrowser
          items={items}
          isLoading={q.isLoading}
          hasMore={!!q.hasNextPage}
          loadingMore={q.isFetchingNextPage}
          onLoadMore={q.fetchNextPage}
          linkTo={linkTo}
          sources={sources}
          groupBy={groupBy}
          emptyLabel="分组是空的 — 在播放页或图片查看器里点「加入分组」"
        />
      )}

      <MoreFooter
        hasNextPage={!!q.hasNextPage}
        isFetchingNextPage={q.isFetchingNextPage}
        fetchNextPage={q.fetchNextPage}
        doneLabel="已加载全部"
        loaded={items.length}
        pageSize={MEDIA_PAGE_SIZE}
      />
    </div>
  );
}
