import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import mpegts from "mpegts.js";

import { api, Channel, MediaCursor, MediaItem, MediaPage, Video } from "../api/client";
import { ChevronLeftIcon, ChevronRightIcon, PlayIcon, StarIcon } from "../components/icons";
import { LoadingState, Spinner, cx } from "../components/ui";

interface VideoResp { video: Video; favorite: boolean }

const MEDIA_ERR_LABEL: Record<number, string> = {
  1: "ABORTED 用户取消加载",
  2: "NETWORK 网络层错误(后端 502/503/连接断开)",
  3: "DECODE 解码失败(浏览器不支持该编码,如 H.265)",
  4: "SRC_NOT_SUPPORTED 服务端返回不能识别为视频",
};

const PLAYLIST_PAGE_SIZE = 500;

// playlistRequest builds the base URL (without the cursor) based on URL params:
//   ?ch=13                  → channel/topic media
//   ?q=foo&ch=13            → search results (optionally channel-scoped)
//   ?text=...&date_from=... → advanced search filters
//   ?fav=1                  → favorites
//
// Every branch hits the merged media endpoints with kind=video: the playlist is
// a queue of things to *play*, and an image in it would have nowhere to go.
function playlistRequest(p: URLSearchParams): string | null {
  const ch = p.get("ch");
  const q = p.get("q");
  const text = p.get("text");
  const fileName = p.get("file_name");
  const dateFrom = p.get("date_from");
  const dateTo = p.get("date_to");
  const fav = p.get("fav");
  // `order` is forwarded to every list endpoint so the playlist ordering
  // matches the page the user came from (and prev/next walk in that order).
  const order = p.get("order");

  const base = () => {
    const qs = new URLSearchParams({ limit: String(PLAYLIST_PAGE_SIZE), kind: "video" });
    if (order) qs.set("order", order);
    return qs;
  };

  if (fav) {
    const qs = base();
    if (fileName) qs.set("file_name", fileName);
    if (dateFrom) qs.set("date_from", dateFrom);
    if (dateTo) qs.set("date_to", dateTo);
    return `/api/favorites/?${qs}`;
  }

  const hasSearch = !!(q || text || fileName || dateFrom || dateTo);
  if (hasSearch) {
    const qs = base();
    if (q) qs.set("q", q);
    if (text) qs.set("text", text);
    if (fileName) qs.set("file_name", fileName);
    if (dateFrom) qs.set("date_from", dateFrom);
    if (dateTo) qs.set("date_to", dateTo);
    if (ch) qs.set("channel_id", ch);
    return `/api/media/search?${qs}`;
  }
  if (ch) {
    const qs = base();
    // Grouped channel view links carry ?streamer=... (possibly empty = the
    // "其它" bucket) — keep the playlist scoped to that streamer.
    if (p.has("streamer")) qs.set("streamer", p.get("streamer") ?? "");
    return `/api/channels/${ch}/media?${qs}`;
  }
  return null;
}

// backTarget works out where "返回" should go from the same URL params that
// built the playlist, so the player returns to the exact list it was opened
// from — with its filters — instead of always to the home page.
//
// Order matters: favorites and the search page both carry file_name/date
// filters, and the search page also sets ?ch when a channel is picked, so
// "fav" is checked first, then the search-page fields, and only then a bare
// channel/topic context (whose own search uses ?q, not ?text/?file_name).
type Back = { to: string; kind: "fav" | "search" | "channel" | "home"; ch?: string };

function backTarget(p: URLSearchParams): Back {
  const pick = (keys: string[], rename: Record<string, string> = {}) => {
    const out = new URLSearchParams();
    for (const k of keys) {
      if (p.has(k)) out.set(rename[k] ?? k, p.get(k) ?? "");
    }
    const qs = out.toString();
    return qs ? `?${qs}` : "";
  };

  if (p.get("fav")) {
    return { kind: "fav", to: `/favorites${pick(["file_name", "date_from", "date_to", "order", "kind"])}` };
  }
  if (p.get("text") || p.get("file_name") || p.get("date_from") || p.get("date_to")) {
    return {
      kind: "search",
      to: `/search${pick(["text", "file_name", "date_from", "date_to", "order", "kind", "ch"], { ch: "channel_id" })}`,
    };
  }
  const ch = p.get("ch");
  if (ch) {
    return { kind: "channel", ch, to: `/channels/${ch}${pick(["q", "order", "kind", "streamer"])}` };
  }
  return { kind: "home", to: "/" };
}

// flipOrder maps a sort key to its reverse, so "the rows before X" can be asked
// for as "the rows after X in the opposite order" — the media endpoints only
// page forward. duration keeps a plain id cursor server-side and has no
// meaningful reverse, so it returns null (the playlist then just starts at the
// top of the list, the old behaviour).
function flipOrder(order: string | null): string | null {
  switch (order ?? "") {
    case "":
    case "date_desc":
      return "date_asc";
    case "date_asc":
      return "date_desc";
    case "name_asc":
      return "name_desc";
    case "name_desc":
      return "name_asc";
    default:
      return null;
  }
}

// PlaylistPage is one fetched slice of the playlist window.
interface PlaylistPage {
  items: MediaItem[];
  hasMore: boolean; // more rows below this page
  next?: number; //    cursor for the next page (last row's video id)
  hasPrev: boolean; // more rows above this page
  // Set on pages loaded upward: the id of the row that followed them when they
  // were fetched. See getNextPageParam — without it a refetch breaks the list.
  continueFrom?: number;
}

type PlaylistParam =
  | { kind: "top" } // start of the list (unflippable order)
  | { kind: "anchor"; id: number } // the opened video, then the rows after it
  | { kind: "after"; id: number }
  | { kind: "before"; id: number; key: string };

function videoToItem(v: Video): MediaItem {
  return { ...v, kind: "video", url: v.stream_url, thumb_url: `/api/videos/${v.id}/thumb` };
}

// withCursor appends the merged-list keyset cursor. Only the video half matters
// here (the playlist is kind=video), but the shape stays the server's.
function withCursor(url: string, cursor: MediaCursor): string {
  if (!cursor.video) return url;
  const sep = url.includes("?") ? "&" : "?";
  return `${url}${sep}offset_video=${cursor.video}`;
}

export function Player() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const qc = useQueryClient();
  const [mediaErr, setMediaErr] = useState<string | null>(null);
  const [streamDiag, setStreamDiag] = useState<string | null>(null);
  const [containerHint, setContainerHint] = useState<string>("");
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const flvPlayerRef = useRef<mpegts.Player | null>(null);
  const sidebarRef = useRef<HTMLDivElement | null>(null);

  // Stable string of the search params; used both as react-query cache key
  // and as a dependency for the "scroll-into-view" effect.
  const playlistKey = searchParams.toString();

  const [endedAtPageEnd, setEndedAtPageEnd] = useState(false);
  // The current FLV/TS has no keyframe index, so it can only seek inside what's
  // already buffered (see the mpegts.js setup below).
  const [seekLimited, setSeekLimited] = useState(false);

  useEffect(() => {
    setMediaErr(null);
    setStreamDiag(null);
    setContainerHint("");
    setEndedAtPageEnd(false);
    setSeekLimited(false);
  }, [id]);

  // Bring the player itself into view when the video changes. There is no
  // scroll reset on navigation anywhere in the app, so opening a tile from far
  // down a grid — or picking the next item from the playlist, which sits BELOW
  // the player on a phone — left the page scrolled and the video off-screen.
  // Only moves when the player is actually out of view, so a desktop user who
  // scrolled down to read the details isn't yanked on every autoplay step.
  const playerRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = playerRef.current;
    if (!el) return;
    const header = document.querySelector("header");
    const offset = (header?.getBoundingClientRect().height ?? 0) + 12;
    const top = el.getBoundingClientRect().top;
    if (top < offset - 1 || top > window.innerHeight * 0.4) {
      window.scrollTo({ top: Math.max(0, window.scrollY + top - offset) });
    }
  }, [id]);

  // Position the highlighted item inside the sidebar — but only by setting
  // the scroller's scrollTop directly, never via scrollIntoView (which can
  // cascade up to ancestor scroll containers and yank the whole page to the
  // top). Strategy: if the current item's box is already inside the visible
  // window, do nothing. Otherwise center it. Offsets are measured from the
  // client rects so nested markup / positioned ancestors can't skew the math.
  //
  // It runs until it has actually positioned the current video, not just once
  // per id change: on open the playlist is usually still loading, so the row
  // doesn't exist yet — the old version gave up right there and never tried
  // again, leaving the list parked at the top. Once positioned for this id it
  // stops, so loading another page doesn't drag the user's scroll back.
  const positionedFor = useRef<string | null>(null);
  useEffect(() => {
    const scroller = sidebarRef.current;
    if (!scroller || !id || positionedFor.current === `${id}|${playlistKey}`) return;
    const el = scroller.querySelector<HTMLElement>(`[data-video-id="${id}"]`);
    if (!el) return;
    positionedFor.current = `${id}|${playlistKey}`;
    const top =
      el.getBoundingClientRect().top -
      scroller.getBoundingClientRect().top +
      scroller.scrollTop;
    const bottom = top + el.offsetHeight;
    const viewTop = scroller.scrollTop;
    const viewBottom = viewTop + scroller.clientHeight;
    if (top >= viewTop && bottom <= viewBottom) {
      return; // already visible — leave the user's scroll position alone
    }
    scroller.scrollTop = Math.max(0, top - scroller.clientHeight / 2 + el.offsetHeight / 2);
  });

  const meta = useQuery<VideoResp>({
    queryKey: ["video", id],
    queryFn: () => api.get(`/api/videos/${id}`),
    enabled: !!id,
    // Keep showing the previous video's metadata while fetching the next, so
    // the layout (especially the playlist sidebar) doesn't unmount/remount
    // and lose its scroll position.
    placeholderData: keepPreviousData,
  });

  // Playlist (siblings from the same context), windowed AROUND the video that
  // was opened rather than always starting at the top of the list.
  //
  // Starting at the top meant the opened video was simply absent whenever it
  // sat past the first page (easy after paging a grid a few times), so the
  // sidebar had nothing to scroll to — and with automatic paging removed the
  // user would have to page manually to find it. Now the first page is the
  // opened video followed by the rows after it, so it is always row one and
  // there is no timing to get wrong. Rows before it load on demand from a
  // button at the top (fetched as "after it, in the reverse order").
  //
  // The anchor is sticky while the user moves through this window (clicking
  // the next item keeps the same list); it only re-anchors when the current
  // video isn't in the loaded window at all.
  const baseURL = playlistRequest(searchParams);
  const order = searchParams.get("order");
  const reverseOrder = flipOrder(order);
  const [anchor, setAnchor] = useState(() => Number(id));
  const playlist = useInfiniteQuery<PlaylistPage>({
    queryKey: ["playlist", playlistKey, reverseOrder ? anchor : "top"],
    enabled: !!baseURL,
    initialPageParam: (reverseOrder ? { kind: "anchor", id: anchor } : { kind: "top" }) as PlaylistParam,
    queryFn: async ({ pageParam }) => {
      const pp = pageParam as PlaylistParam;
      if (!baseURL) return { items: [], hasMore: false, hasPrev: false };

      if (pp.kind === "before") {
        const url = new URL(baseURL, window.location.origin);
        url.searchParams.set("order", reverseOrder!);
        url.searchParams.set("offset_video", String(pp.id));
        const resp = await api.get<MediaPage>(url.pathname + url.search);
        // NULLS LAST holds in both directions, so the reverse query also
        // returns the empty-key tail — rows that really sort at the very END of
        // the list. Drop them when the boundary row has a key.
        const keyOf = (m: MediaItem) =>
          (order ?? "").startsWith("name") ? m.file_name ?? "" : m.date ?? "";
        const rows = resp.items.filter((m) => keyOf(m) !== "");
        return { items: rows.reverse(), hasMore: true, continueFrom: pp.id, hasPrev: resp.has_more };
      }

      if (pp.kind === "anchor") {
        const [meta, rest] = await Promise.all([
          api.get<VideoResp>(`/api/videos/${pp.id}`),
          api.get<MediaPage>(withCursor(baseURL, { video: pp.id })),
        ]);
        return {
          items: [videoToItem(meta.video), ...rest.items],
          hasMore: rest.has_more,
          next: rest.next.video ?? pp.id,
          hasPrev: true, // unknown until asked; the button hides once it returns nothing
        };
      }

      const cursor = pp.kind === "after" ? { video: pp.id } : {};
      const resp = await api.get<MediaPage>(withCursor(baseURL, cursor));
      return { items: resp.items, hasMore: resp.has_more, next: resp.next.video, hasPrev: false };
    },
    // A refetch (TanStack's infiniteQueryBehavior) replays page 0 from its
    // stored param and derives every later page from getNextPageParam. Once a
    // page has been loaded upward, page 0 is that "before" page — so it must be
    // able to name what comes after it, or the refetch stops there and silently
    // drops the opened video and everything below. continueFrom re-anchors on
    // the row that followed it, which also works when the page came back empty.
    getNextPageParam: (last) => {
      if (last.continueFrom != null) return { kind: "anchor", id: last.continueFrom } as PlaylistParam;
      return last.hasMore && last.next ? ({ kind: "after", id: last.next } as PlaylistParam) : undefined;
    },
    // Several 500-row pages re-downloaded on every app switch would be wasted
    // work on a phone and could shift rows under the user's thumb.
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    getPreviousPageParam: (first) => {
      const head = first.items[0];
      if (!first.hasPrev || !head || !reverseOrder) return undefined;
      const key = (order ?? "").startsWith("name") ? head.file_name ?? "" : head.date ?? "";
      // The reverse-order trick is only exact for a boundary row that HAS a sort
      // key. For an empty one (a video with no file name under a name sort) the
      // server's keyset returns only other empty-key rows, silently skipping
      // every keyed row that really comes before it — a wrong list is worse
      // than no button, so offer none.
      if (key === "") return undefined;
      return { kind: "before", id: head.id, key } as PlaylistParam;
    },
  });

  const list = useMemo<MediaItem[]>(
    () => playlist.data?.pages.flatMap((p) => p.items) ?? [],
    [playlist.data],
  );
  const currentIdx = useMemo(
    () => list.findIndex((v) => String(v.id) === id),
    [list, id],
  );
  const next = currentIdx >= 0 && currentIdx < list.length - 1 ? list[currentIdx + 1] : null;
  const prev = currentIdx > 0 ? list[currentIdx - 1] : null;

  useEffect(() => {
    if (!reverseOrder || playlist.isFetching || list.length === 0 || currentIdx >= 0) return;
    setAnchor(Number(id));
  }, [reverseOrder, playlist.isFetching, list.length, currentIdx, id]);

  // Prepending the previous page would push everything down and scroll the
  // current row out of view; keep the viewport where it was by offsetting
  // scrollTop by exactly the height that was inserted above.
  const prependFrom = useRef<{ height: number; top: number } | null>(null);
  const loadPrevious = () => {
    const sc = sidebarRef.current;
    if (sc) prependFrom.current = { height: sc.scrollHeight, top: sc.scrollTop };
    playlist.fetchPreviousPage();
  };
  useLayoutEffect(() => {
    const from = prependFrom.current;
    const sc = sidebarRef.current;
    if (!from || !sc || playlist.isFetchingPreviousPage) return;
    sc.scrollTop = from.top + (sc.scrollHeight - from.height);
    prependFrom.current = null;
  }, [list.length, playlist.isFetchingPreviousPage]);

  // No automatic paging here: the next page loads only from the button at the
  // bottom of the playlist. (It used to prefetch near the end and on scroll.)
  // Autoplay therefore stops at the last loaded video instead of silently
  // pulling another 500 rows.
  const atLoadedEnd = currentIdx >= 0 && currentIdx === list.length - 1 && !!playlist.hasNextPage;

  // Container detection + player setup
  useEffect(() => {
    const video = videoRef.current;
    const url = meta.data?.video.stream_url;
    if (!video || !url) return;

    let cancelled = false;
    setMediaErr(null);
    setStreamDiag(null);

    (async () => {
      let kind: "flv" | "mpegts" | "native" = "native";
      let hint = "";
      try {
        const r = await fetch(url, {
          headers: { Range: "bytes=0-15" },
          credentials: "include",
        });
        if (r.ok) {
          const ab = await r.arrayBuffer();
          const b = new Uint8Array(ab);
          if (b.length >= 3 && b[0] === 0x46 && b[1] === 0x4c && b[2] === 0x56) {
            kind = "flv";
            hint = "FLV — mpegts.js";
          } else if (b.length >= 8 && b[4] === 0x66 && b[5] === 0x74 && b[6] === 0x79 && b[7] === 0x70) {
            hint = "MP4 (ftyp)";
          } else if (b.length >= 1 && b[0] === 0x47) {
            // MPEG-TS sync byte (0x47) at offset 0 — naked .ts stream.
            kind = "mpegts";
            hint = "MPEG-TS — mpegts.js";
          } else {
            hint = "未识别容器,试试 native";
          }
        }
      } catch {
        // probe failed
      }
      if (cancelled) return;
      setContainerHint(hint);

      if (kind === "flv" || kind === "mpegts") {
        if (!mpegts.getFeatureList().mseLivePlayback) {
          setMediaErr("浏览器不支持 MSE — 该视频(FLV/TS)无法播放");
          return;
        }
        const player = mpegts.createPlayer(
          {
            type: kind, // "flv" | "mpegts"
            url,
            isLive: false,
            cors: true,
            withCredentials: true,
          },
          {
            // Default is false: a seek into an unbuffered range then lands on
            // the nearest keyframe, not the second the user dragged to — up to
            // a whole GOP off, several seconds on typical live recordings. With
            // it on, mpegts.js decodes from that keyframe and drops frames up to
            // the exact target.
            accurateSeek: true,
          },
        );
        // mpegts.js can only seek outside the buffered range when the file
        // carries a keyframe index (FLV onMetaData.keyframes; TS never has
        // one) — MediaInfo.isSeekable() is literally hasKeyframesIndex, and
        // without it the transmuxer's seek() returns without fetching anything.
        // That's a property of the file, not something config can fix, so tell
        // the user why a far drag won't take.
        player.on(mpegts.Events.MEDIA_INFO, (info: { hasKeyframesIndex?: boolean | null }) => {
          if (!cancelled && info?.hasKeyframesIndex !== true) setSeekLimited(true);
        });
        player.attachMediaElement(video);
        player.on(mpegts.Events.ERROR, (errType, errDetail, errInfo) => {
          setMediaErr(`mpegts ${errType}: ${errDetail}`);
          setStreamDiag(JSON.stringify(errInfo));
        });
        player.load();
        flvPlayerRef.current = player;
        try { await player.play(); } catch { /* autoplay block */ }
      } else {
        video.src = url;
        try { await video.play(); } catch { /* autoplay block */ }
      }
    })();

    return () => {
      cancelled = true;
      if (flvPlayerRef.current) {
        try { flvPlayerRef.current.destroy(); } catch { /* noop */ }
        flvPlayerRef.current = null;
      }
      try {
        video.pause();
        video.removeAttribute("src");
        video.load();
      } catch { /* noop */ }
    };
  }, [meta.data?.video.stream_url]);

  const fav = useMutation({
    mutationFn: async () => {
      if (!meta.data) return;
      if (meta.data.favorite) {
        await api.del(`/api/favorites/${meta.data.video.id}`);
      } else {
        await api.post(`/api/favorites/`, { video_id: meta.data.video.id });
      }
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["video", id] }),
  });

  const goToVideo = (vid: number) => {
    const params = searchParams.toString();
    navigate(`/videos/${vid}${params ? "?" + params : ""}`);
  };

  const v = meta.data?.video;
  const hasPlaylist = list.length > 0;

  const back = useMemo(() => backTarget(searchParams), [searchParams]);
  // Name the channel/topic we're returning to. Usually already cached from the
  // page the user just came from, so this rarely costs a request.
  const backChannel = useQuery<{ channel: Channel }>({
    queryKey: ["channel", back.ch],
    queryFn: () => api.get(`/api/channels/${back.ch}`),
    enabled: back.kind === "channel",
  });
  const backLabel =
    back.kind === "fav"
      ? "返回收藏"
      : back.kind === "search"
        ? "返回搜索结果"
        : back.kind === "channel"
          ? backChannel.data?.channel
            ? `返回${backChannel.data.channel.dialog_kind === "topic" ? "话题" : "频道"}「${backChannel.data.channel.title}」`
            : "返回"
          : "返回我的频道";
  const showLoading = meta.isLoading && !meta.data;
  const showNotFound = !meta.isLoading && !meta.data;
  const isFav = !!meta.data?.favorite;

  return (
    <div className="p-4 md:p-6">
      <div className={cx("grid gap-5", hasPlaylist && "xl:grid-cols-[minmax(0,1fr)_360px]")}>
        <div className="min-w-0 space-y-5">
          {/* video stage — media keeps its own near-black backdrop inside the
              card frame; the chrome around it stays on the neutral canvas. */}
          <div ref={playerRef} className="card overflow-hidden">
            {endedAtPageEnd && (
              <div className="flex flex-wrap items-center gap-2 border-b border-gray-200 px-4 py-2 text-theme-xs text-gray-600 dark:border-gray-800 dark:text-gray-300">
                已播完当前已加载的列表。
                <button
                  onClick={async () => {
                    setEndedAtPageEnd(false);
                    await playlist.fetchNextPage();
                  }}
                  disabled={playlist.isFetchingNextPage}
                  className="btn btn-outline btn-sm"
                >
                  加载下一页继续播放
                </button>
              </div>
            )}
            <div className="relative flex min-h-[40vh] items-center justify-center bg-gray-950">
              {showLoading && <LoadingState />}
              {showNotFound && (
                <div className="p-6 text-theme-sm text-error-400">未找到视频</div>
              )}
              {v && (
                <>
                  <video
                    key={v.id}
                    ref={videoRef}
                    controls
                    preload="metadata"
                    className="max-h-[78vh] max-w-full outline-none"
                    onEnded={() => {
                      if (next) goToVideo(next.id);
                      else if (atLoadedEnd) setEndedAtPageEnd(true);
                    }}
                    onError={async (e) => {
                      const code = (e.currentTarget.error?.code ?? 0);
                      setMediaErr(MEDIA_ERR_LABEL[code] ?? `未知(code=${code})`);
                      try {
                        const r = await fetch(v.stream_url, { credentials: "include" });
                        const ct = r.headers.get("content-type") ?? "(none)";
                        const cl = r.headers.get("content-length") ?? "(none)";
                        const cr = r.headers.get("content-range") ?? "(none)";
                        const ab = await r.arrayBuffer();
                        const head = Array.from(new Uint8Array(ab.slice(0, 16)))
                          .map((b) => b.toString(16).padStart(2, "0"))
                          .join(" ");
                        setStreamDiag(
                          `HTTP ${r.status} ${r.statusText}\n` +
                          `Content-Type: ${ct}\nContent-Length: ${cl}\nContent-Range: ${cr}\n` +
                          `Body bytes: ${ab.byteLength}\nFirst 16 bytes (hex): ${head}`,
                        );
                      } catch (err: any) {
                        setStreamDiag(`fetch failed: ${err.message ?? err}`);
                      }
                    }}
                  />
                  {mediaErr && (
                    <div className="absolute inset-x-0 bottom-0 space-y-1 border-l-4 border-error-500 bg-gray-900/95 p-3 text-theme-xs text-error-300 backdrop-blur">
                      <div className="font-medium">播放失败: {mediaErr}</div>
                      {streamDiag && (
                        <pre className="whitespace-pre-wrap font-mono text-[11px] text-gray-400">
                          {streamDiag}
                        </pre>
                      )}
                      <div className="break-all text-gray-500">stream_url: {v.stream_url}</div>
                    </div>
                  )}
                </>
              )}
            </div>
          </div>

          {/* metadata + controls */}
          {v && meta.data && (
            <div className="card space-y-4 p-5">
              <div className="flex flex-wrap items-start gap-4">
                <div className="min-w-[240px] flex-1 space-y-1.5">
                  {v.file_name && (
                    <div className="break-all font-medium text-gray-800 dark:text-white/90">
                      {v.file_name}
                    </div>
                  )}
                  <div className="whitespace-pre-wrap break-words text-theme-sm text-gray-600 dark:text-gray-300">
                    {v.text?.trim() || <span className="text-gray-400">无说明</span>}
                  </div>
                </div>

                <div className="flex shrink-0 flex-col gap-2">
                  <button
                    onClick={() => fav.mutate()}
                    disabled={fav.isPending}
                    className={cx(
                      "btn",
                      isFav
                        ? "bg-brand-50 text-brand-500 ring-1 ring-inset ring-brand-200 hover:bg-brand-100 dark:bg-brand-500/[0.12] dark:text-brand-400 dark:ring-brand-500/30"
                        : "btn-primary",
                    )}
                  >
                    <StarIcon filled={isFav} className="size-4" />
                    {isFav ? "已收藏" : "收藏"}
                  </button>
                  {hasPlaylist && (
                    <div className="flex gap-2">
                      <button
                        onClick={() => prev && goToVideo(prev.id)}
                        disabled={!prev}
                        className="btn btn-outline btn-sm flex-1"
                      >
                        <ChevronLeftIcon className="size-4" />
                        上一个
                      </button>
                      <button
                        onClick={() => next && goToVideo(next.id)}
                        disabled={!next}
                        className="btn btn-outline btn-sm flex-1"
                      >
                        下一个
                        <ChevronRightIcon className="size-4" />
                      </button>
                    </div>
                  )}
                </div>
              </div>

              <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t border-gray-200 pt-3 text-theme-xs text-gray-500 dark:border-gray-800 dark:text-gray-400">
                {v.width > 0 && <span>{v.width}×{v.height}</span>}
                {v.file_size > 0 && <span>{(v.file_size / 1024 / 1024).toFixed(1)} MB</span>}
                {v.mime_type && <span>{v.mime_type}</span>}
                {v.date && <span className="tabular-nums">{v.date.slice(0, 19).replace("T", " ")}</span>}
                {v.from && <span>from: {v.from}</span>}
                {containerHint && <span className="badge badge-info">{containerHint}</span>}
                {seekLimited && (
                  <span
                    className="badge badge-warning"
                    title="该文件没有关键帧索引(常见于直播录像 FLV),播放器无法计算远处时间点对应的文件位置,只能在已缓冲的范围内拖动进度条"
                  >
                    无关键帧索引 · 只能在已缓冲范围内拖动
                  </span>
                )}
                <Link
                  to={back.to}
                  className="ml-auto inline-flex max-w-full items-center gap-1 truncate hover:text-gray-700 dark:hover:text-gray-200"
                >
                  <ChevronLeftIcon className="size-4 shrink-0" />
                  <span className="truncate">{backLabel}</span>
                </Link>
              </div>
            </div>
          )}
        </div>

        {/* playlist sidebar */}
        {hasPlaylist && (
          <aside className="card flex max-h-[80vh] min-h-0 flex-col overflow-hidden xl:sticky xl:top-[84px]">
            <div className="flex shrink-0 items-center gap-2 border-b border-gray-200 px-4 py-3 dark:border-gray-800">
              <span className="text-theme-sm font-medium text-gray-800 dark:text-white/90">
                播放列表
              </span>
              <span className="badge badge-gray tabular-nums">已加载 {list.length}</span>
            </div>

            <div ref={sidebarRef} className="custom-scrollbar min-h-0 flex-1 overflow-y-auto">
              {playlist.hasPreviousPage && (
                <div className="flex justify-center border-b border-gray-100 px-3 py-2.5 dark:border-gray-800/60">
                  <button
                    onClick={loadPrevious}
                    disabled={playlist.isFetchingPreviousPage}
                    className="btn btn-outline btn-sm"
                  >
                    {playlist.isFetchingPreviousPage ? (
                      <>
                        <Spinner className="size-3.5" />
                        加载中…
                      </>
                    ) : (
                      "加载上一页"
                    )}
                  </button>
                </div>
              )}
              {list.map((item) => {
                const isCurrent = String(item.id) === id;
                return (
                  <button
                    key={item.id}
                    data-video-id={item.id}
                    onClick={() => goToVideo(item.id)}
                    className={cx(
                      "flex w-full items-start gap-2.5 border-b border-gray-100 px-4 py-2.5 text-left transition-colors dark:border-gray-800/60",
                      isCurrent
                        ? "bg-brand-50 dark:bg-brand-500/[0.12]"
                        : "hover:bg-gray-50 dark:hover:bg-white/[0.04]",
                    )}
                  >
                    <span
                      className={cx(
                        "mt-0.5 w-6 shrink-0 text-theme-xs tabular-nums",
                        isCurrent
                          ? "text-brand-500 dark:text-brand-400"
                          : "text-gray-400 dark:text-gray-500",
                      )}
                    >
                      {isCurrent && <PlayIcon className="size-3" />}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div
                        className={cx(
                          "line-clamp-2 break-all text-theme-xs leading-snug",
                          isCurrent
                            ? "font-medium text-brand-600 dark:text-brand-400"
                            : "text-gray-700 dark:text-gray-300",
                        )}
                      >
                        {item.file_name?.trim() || item.text?.trim() || `视频 #${item.id}`}
                      </div>
                      <div className="mt-0.5 text-[11px] leading-4 text-gray-400 dark:text-gray-500">
                        {item.duration_seconds > 0 && fmtDur(item.duration_seconds)}
                        {item.date && " · " + item.date.slice(0, 10)}
                      </div>
                    </div>
                  </button>
                );
              })}

              <div className="flex flex-col items-center justify-center gap-2 px-3 py-3 text-theme-xs text-gray-400 dark:text-gray-500">
                {playlist.hasNextPage ? (
                  <button
                    onClick={() => playlist.fetchNextPage()}
                    disabled={playlist.isFetchingNextPage}
                    className="btn btn-outline btn-sm"
                  >
                    {playlist.isFetchingNextPage ? (
                      <>
                        <Spinner className="size-3.5" />
                        加载中…
                      </>
                    ) : (
                      `加载下一页 (+${PLAYLIST_PAGE_SIZE})`
                    )}
                  </button>
                ) : list.length > 0 ? (
                  "— 已加载全部 —"
                ) : null}
              </div>
            </div>
          </aside>
        )}
      </div>
    </div>
  );
}

function fmtDur(s: number) {
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h) return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return `${m}:${String(sec).padStart(2, "0")}`;
}
