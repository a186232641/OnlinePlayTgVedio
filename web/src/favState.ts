import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSyncExternalStore } from "react";

import { api } from "./api/client";

// Favorite state changed in this session, keyed "video:12" / "photo:7".
//
// A tile's star starts from the list's `favorite` field, but lists stay cached
// (for scroll restoration) long after a toggle — in the grid, the image viewer
// or the player. Every toggle records its outcome here so each of those shows
// the same state without refetching whole lists.
const overrides = new Map<string, boolean>();
const listeners = new Set<() => void>();

const keyOf = (kind: "video" | "photo", id: number) => `${kind}:${id}`;

export function setFavoriteState(kind: "video" | "photo", id: number, fav: boolean) {
  overrides.set(keyOf(kind, id), fav);
  listeners.forEach((l) => l());
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

// useFavoriteState is `known` (from the server) unless toggled since.
export function useFavoriteState(kind: "video" | "photo", id: number | undefined, known: boolean): boolean {
  const o = useSyncExternalStore(subscribe, () => (id == null ? undefined : overrides.get(keyOf(kind, id))));
  return o ?? known;
}

// useToggleFavorite adds/removes a favorite and publishes the result. The
// favorites lists and the item's detail query are invalidated so they catch up.
export function useToggleFavorite() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ kind, id, fav }: { kind: "video" | "photo"; id: number; fav: boolean }) => {
      if (fav) {
        if (kind === "photo") await api.del(`/api/favorites/photo/${id}`);
        else await api.del(`/api/favorites/${id}`);
      } else {
        await api.post("/api/favorites/", { kind, id });
      }
      return { kind, id, now: !fav };
    },
    onSuccess: ({ kind, id, now }) => {
      setFavoriteState(kind, id, now);
      qc.invalidateQueries({ queryKey: ["favorites"] });
      qc.invalidateQueries({ queryKey: [kind, kind === "video" ? String(id) : id] });
    },
    onError: (e: Error) => alert(`收藏操作失败: ${e.message}`),
  });
}
