import { useInfiniteQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { api, MediaCursor, MediaItem, MediaKindFilter, MediaPage, MediaSource } from "./client";

export const MEDIA_PAGE_SIZE = 120;

// normalizeKind coerces a ?kind= URL value to a known filter ("" = both).
export function normalizeKind(s: string | null): MediaKindFilter {
  return s === "video" || s === "photo" ? s : "";
}

// useMediaPages wraps the merged video+image listing in one infinite query.
//
// Merged lists page with TWO cursors (one per table — see internal/db/media.go),
// so the page param is a MediaCursor object rather than a single id, and "is
// there more" comes from the server's has_more instead of a short-page guess:
// with two branches merged and truncated, a short page no longer means the end.
export function useMediaPages(
  key: unknown[],
  buildQuery: () => URLSearchParams,
  opts: { path: string; enabled?: boolean } ,
) {
  const q = useInfiniteQuery<MediaPage>({
    queryKey: key,
    enabled: opts.enabled ?? true,
    initialPageParam: {} as MediaCursor,
    queryFn: ({ pageParam }) => {
      const cursor = pageParam as MediaCursor;
      const qs = buildQuery();
      qs.set("limit", String(MEDIA_PAGE_SIZE));
      if (cursor.video) qs.set("offset_video", String(cursor.video));
      if (cursor.photo) qs.set("offset_photo", String(cursor.photo));
      return api.get<MediaPage>(`${opts.path}?${qs}`);
    },
    getNextPageParam: (last) => (last.has_more ? last.next : undefined),
  });

  const items = useMemo<MediaItem[]>(
    () => q.data?.pages.flatMap((p) => p.items) ?? [],
    [q.data],
  );
  // Each page names only its own items' channels; merge them so every loaded
  // item can resolve its origin.
  const sources = useMemo<Record<string, MediaSource>>(
    () => Object.assign({}, ...(q.data?.pages.map((p) => p.sources ?? {}) ?? [])),
    [q.data],
  );
  const first = q.data?.pages[0];
  return {
    query: q,
    items,
    sources,
    totalVideos: first?.total_videos,
    totalPhotos: first?.total_photos,
  };
}
