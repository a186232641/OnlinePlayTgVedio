import { useState } from "react";

import { FolderIcon, ImageIcon, PlayIcon } from "./icons";

// Cover is the thumbnail of a group card (a favorites group, a collection):
// its latest item's thumb, degrading to a typed placeholder when that has no
// thumbnail — or to a folder when the group is empty (no src).
export function Cover({ kind, src }: { kind?: "video" | "photo"; src?: string }) {
  const [failed, setFailed] = useState(false);
  if (!src || failed) {
    const Icon = !src ? FolderIcon : kind === "video" ? PlayIcon : ImageIcon;
    return (
      <div className="flex size-full items-center justify-center bg-gray-100 text-gray-300 dark:bg-white/[0.04] dark:text-gray-600">
        <Icon className="size-8" />
      </div>
    );
  }
  return (
    <img
      src={src}
      alt=""
      loading="lazy"
      decoding="async"
      onError={() => setFailed(true)}
      className="size-full bg-gray-100 object-cover transition-transform duration-200 group-hover:scale-[1.03] dark:bg-white/[0.04]"
    />
  );
}
