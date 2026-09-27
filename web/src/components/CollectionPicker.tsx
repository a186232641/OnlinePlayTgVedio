import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FormEvent, useEffect, useRef, useState } from "react";

import { api, Collection } from "../api/client";
import { CheckIcon, FolderIcon, PlusIcon } from "./icons";
import { cx } from "./ui";

// CollectionPicker is the "加入分组" button: a popover listing the user's
// collections with a tick on those holding this video/image, where clicking a
// row adds or removes it, plus an inline "新建分组" that creates a group and
// adds the item to it in one go.
//
// Collections are independent of favorites — nothing here favorites or pins.
//
// `variant` matches the host: "page" for the player's light chrome, "overlay"
// for the image viewer's dark bar.
export function CollectionPicker({
  kind,
  id,
  variant = "page",
  className,
}: {
  kind: "video" | "photo";
  id: number;
  variant?: "page" | "overlay";
  className?: string;
}) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const root = useRef<HTMLDivElement | null>(null);

  const list = useQuery<{ items: Collection[] }>({
    queryKey: ["collections", "picker", kind, id],
    queryFn: () => api.get(`/api/collections/?kind=${kind}&id=${id}`),
    enabled: open,
  });

  // Close on a click anywhere outside the popover.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent | TouchEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("touchstart", onDown);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("touchstart", onDown);
    };
  }, [open]);

  const done = () => qc.invalidateQueries({ queryKey: ["collections"] });

  const toggle = useMutation({
    mutationFn: (c: Collection) =>
      c.contains
        ? api.del(`/api/collections/${c.id}/items/${kind}/${id}`)
        : api.post(`/api/collections/${c.id}/items`, { kind, id }),
    onSuccess: done,
    onError: (e: Error) => alert(`分组操作失败: ${e.message}`),
  });

  const create = useMutation({
    mutationFn: async (n: string) => {
      const c = await api.post<Collection>("/api/collections/", { name: n });
      await api.post(`/api/collections/${c.id}/items`, { kind, id });
    },
    onSuccess: () => {
      setName("");
      done();
    },
    onError: (e: Error) => alert(`新建分组失败: ${e.message}`),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const n = name.trim();
    if (n) create.mutate(n);
  };

  const items = list.data?.items ?? [];
  const inCount = items.filter((c) => c.contains).length;

  return (
    <div ref={root} className={cx("relative", className)}>
      {variant === "page" ? (
        <button type="button" onClick={() => setOpen((o) => !o)} className="btn btn-outline w-full">
          <FolderIcon className="size-4" />
          {open && inCount > 0 ? `已在 ${inCount} 个分组` : "加入分组"}
        </button>
      ) : (
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          title="加入分组"
          className={cx(
            "flex size-10 items-center justify-center rounded-full text-white transition-colors",
            open ? "bg-white/25" : "bg-white/10 hover:bg-white/20",
          )}
        >
          <FolderIcon className="size-5" />
        </button>
      )}

      {open && (
        <div className="absolute right-0 top-full z-50 mt-2 w-64 rounded-2xl border border-gray-200 bg-white p-2 text-gray-700 shadow-theme-lg dark:border-gray-800 dark:bg-gray-dark dark:text-gray-300">
          <div className="px-2 pb-1.5 pt-1 text-theme-xs font-medium text-gray-500 dark:text-gray-400">
            加入分组
          </div>
          <div className="custom-scrollbar max-h-64 overflow-y-auto">
            {list.isLoading ? (
              <div className="px-2 py-3 text-theme-xs text-gray-400">加载中…</div>
            ) : items.length === 0 ? (
              <div className="px-2 py-3 text-theme-xs text-gray-400">还没有分组,在下面新建一个</div>
            ) : (
              items.map((c) => (
                <button
                  key={c.id}
                  type="button"
                  onClick={() => toggle.mutate(c)}
                  disabled={toggle.isPending}
                  className="flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-theme-sm transition-colors hover:bg-gray-100 disabled:opacity-60 dark:hover:bg-white/[0.06]"
                >
                  <span
                    className={cx(
                      "flex size-4 shrink-0 items-center justify-center rounded border",
                      c.contains
                        ? "border-brand-500 bg-brand-500 text-white"
                        : "border-gray-300 dark:border-gray-600",
                    )}
                  >
                    {c.contains && <CheckIcon className="size-3" />}
                  </span>
                  <span className="min-w-0 flex-1 truncate">{c.name}</span>
                  <span className="shrink-0 text-theme-xs tabular-nums text-gray-400">
                    {c.videos + c.photos}
                  </span>
                </button>
              ))
            )}
          </div>
          <form onSubmit={submit} className="mt-1 flex gap-1.5 border-t border-gray-200 px-1 pt-2 dark:border-gray-800">
            <input
              className="field field-sm min-w-0 flex-1"
              placeholder="新建分组…"
              maxLength={64}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <button className="btn btn-primary btn-sm" disabled={!name.trim() || create.isPending} title="新建并加入">
              <PlusIcon className="size-4" />
            </button>
          </form>
        </div>
      )}
    </div>
  );
}
