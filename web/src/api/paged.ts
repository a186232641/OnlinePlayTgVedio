import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";

import { api, SyncState } from "./client";

// Page size for the small limit/offset lists (topics, streamers, a session's
// channels). Small enough that a phone renders a page without stalling.
export const LIST_PAGE_SIZE = 50;

// useDebounced delays a fast-changing value (a search box) so typing sends one
// request when the user pauses, not one per keystroke.
export function useDebounced<T>(value: T, ms = 300): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

interface PagedResp {
  has_more?: boolean;
  total?: number;
}

// usePagedList drives a limit/offset list endpoint as an infinite query.
//
// The page param is the number of rows already loaded. Those lists sort on keys
// that move while a sync runs (media counts, last-synced time), so a row can
// shift across a page boundary between requests; flattening dedupes by key so
// that shows up at worst as a row appearing a page later, never twice.
export function usePagedList<T, R extends PagedResp>(opts: {
  key: unknown[];
  path: string;
  params: URLSearchParams;
  pick: (resp: R) => T[];
  keyOf: (item: T) => string | number;
  pageSize?: number;
  enabled?: boolean;
}) {
  const pageSize = opts.pageSize ?? LIST_PAGE_SIZE;
  const q = useInfiniteQuery<R>({
    queryKey: opts.key,
    enabled: opts.enabled ?? true,
    initialPageParam: 0,
    queryFn: ({ pageParam }) => {
      const qs = new URLSearchParams(opts.params);
      qs.set("limit", String(pageSize));
      qs.set("offset", String(pageParam as number));
      const sep = opts.path.includes("?") ? "&" : "?";
      return api.get<R>(`${opts.path}${sep}${qs}`);
    },
    getNextPageParam: (last, all) =>
      last.has_more ? all.reduce((n, p) => n + opts.pick(p).length, 0) : undefined,
  });

  const items = useMemo(() => {
    const seen = new Set<string | number>();
    const out: T[] = [];
    for (const page of q.data?.pages ?? []) {
      for (const it of opts.pick(page)) {
        const k = opts.keyOf(it);
        if (seen.has(k)) continue;
        seen.add(k);
        out.push(it);
      }
    }
    return out;
    // pick/keyOf are stable per call site; only the data drives this.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q.data]);

  return { query: q, items, total: q.data?.pages[0]?.total, pageSize };
}

// useSyncStatuses polls the live sync state of many channels/topics in ONE
// request, and only while something is running.
//
// This replaces two patterns that both hurt on a long list: re-fetching the
// whole list every two seconds (re-renders every row), and one status query per
// row (a request per row on every page load).
//
// onFinished fires with the ids whose sync just ended, so the page can refresh
// the counts it shows for them.
export function useSyncStatuses(ids: number[], onFinished?: (ids: number[]) => void) {
  const idKey = useMemo(() => [...ids].sort((a, b) => a - b).join(","), [ids]);
  const q = useQuery<{ states: Record<string, SyncState> }>({
    queryKey: ["sync-statuses", idKey],
    queryFn: () => api.get(`/api/channels/sync-status?ids=${idKey}`),
    enabled: idKey.length > 0,
    refetchInterval: (query) =>
      Object.values(query.state.data?.states ?? {}).some((s) => s.running) ? 2000 : false,
  });
  const states = q.data?.states ?? {};

  const prevRunning = useRef<Set<string>>(new Set());
  useEffect(() => {
    const now = new Set(Object.keys(states).filter((id) => states[id].running));
    const finished = [...prevRunning.current].filter((id) => !now.has(id)).map(Number);
    prevRunning.current = now;
    if (finished.length > 0) onFinished?.(finished);
    // Only a change in the fetched states matters here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q.data]);

  const anyRunning = Object.values(states).some((s) => s.running);
  return { states, anyRunning };
}
