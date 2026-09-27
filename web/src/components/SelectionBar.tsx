import { useMemo, useState } from "react";

import { MediaItem } from "../api/client";
import { AddToCollectionMenu } from "./CollectionPicker";
import { CheckIcon } from "./icons";
import { Selection, mediaKey } from "./MediaGrid";
import { cx } from "./ui";

// useMediaSelection is a media list's multi-select ("多选") mode: while on,
// tiles toggle into `picked` instead of opening, and a SelectionBar adds the
// picks to a collection. Picks survive filter changes and paging, so one batch
// can gather from several views of the same page.
//
// Pass `selection` / `selectMany` to MediaBrowser, render SelectToggle where
// the list's controls are and SelectionBar as the page section's last child.
export function useMediaSelection() {
  const [selecting, setSelecting] = useState(false);
  const [picked, setPicked] = useState<Map<string, MediaItem>>(() => new Map());
  const [notice, setNotice] = useState("");

  const selection = useMemo<Selection | undefined>(
    () =>
      selecting
        ? {
            selected: new Set(picked.keys()),
            toggle: (m) =>
              setPicked((prev) => {
                const next = new Map(prev);
                if (next.has(mediaKey(m))) next.delete(mediaKey(m));
                else next.set(mediaKey(m), m);
                return next;
              }),
          }
        : undefined,
    [selecting, picked],
  );

  const selectMany = (list: MediaItem[], on: boolean) =>
    setPicked((prev) => {
      const next = new Map(prev);
      for (const m of list) {
        if (on) next.set(mediaKey(m), m);
        else next.delete(mediaKey(m));
      }
      return next;
    });

  const clear = () => {
    setPicked(new Map());
    setNotice("");
  };
  const end = () => {
    setSelecting(false);
    clear();
  };

  return {
    selecting,
    picked,
    notice,
    selection,
    selectMany,
    start: () => setSelecting(true),
    end,
    clear,
    added: (name: string, n: number) => {
      setPicked(new Map());
      setNotice(`已加入「${name}」,新增 ${n} 项`);
    },
  };
}

export type MediaSelection = ReturnType<typeof useMediaSelection>;

// SelectToggle turns multi-select on and off.
export function SelectToggle({ sel, className }: { sel: MediaSelection; className?: string }) {
  return (
    <button
      type="button"
      onClick={() => (sel.selecting ? sel.end() : sel.start())}
      className={cx(
        "btn",
        sel.selecting
          ? "bg-brand-50 text-brand-500 ring-1 ring-inset ring-brand-200 hover:bg-brand-100 dark:bg-brand-500/[0.12] dark:text-brand-400 dark:ring-brand-500/30"
          : "btn-outline",
        className,
      )}
    >
      <CheckIcon className="size-4" />
      {sel.selecting ? "退出多选" : "多选"}
    </button>
  );
}

// SelectionBar is the multi-select action bar. Render it as the LAST child of
// the list's section: sticky-bottom keeps it on screen while the list above
// scrolls, and parks it after the footer at the end. `items` is what "全选已加载"
// selects — the list as currently loaded.
export function SelectionBar({ sel, items }: { sel: MediaSelection; items: MediaItem[] }) {
  if (!sel.selecting) return null;
  return (
    <div className="card sticky bottom-4 z-20 flex flex-wrap items-center gap-2 p-3 shadow-theme-lg">
      <span className="text-theme-sm font-medium text-gray-800 dark:text-white/90">
        已选 <span className="tabular-nums">{sel.picked.size}</span> 项
      </span>
      {sel.notice && <span className="text-theme-xs text-success-600 dark:text-success-500">{sel.notice}</span>}
      <div className="ml-auto flex flex-wrap items-center gap-2">
        <button type="button" className="btn btn-outline btn-sm" onClick={() => sel.selectMany(items, true)}>
          全选已加载
        </button>
        <button type="button" className="btn btn-outline btn-sm" disabled={sel.picked.size === 0} onClick={sel.clear}>
          清空选择
        </button>
        <AddToCollectionMenu items={[...sel.picked.values()]} onAdded={sel.added} />
        <button type="button" className="btn btn-ghost btn-sm" onClick={sel.end}>
          完成
        </button>
      </div>
    </div>
  );
}
