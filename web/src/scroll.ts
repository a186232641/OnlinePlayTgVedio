import { useEffect, useRef } from "react";
import { useLocation, useNavigationType } from "react-router-dom";

// Link state that marks a "返回…" link (BackBar sets it): arriving through it
// restores the scroll position the target page had when the user left it.
export const RESTORE_SCROLL = { restoreScroll: true } as const;

// useReturnScroll puts a page back where it was when the user comes back to it.
//
// Nothing else does: the app uses <BrowserRouter> (no data router, so no
// <ScrollRestoration>), and its "返回" links push a new entry rather than going
// back. Two cases, both keyed off where the user left from:
//
// - Drilling in and using a "返回…" link (topic list → topic → 返回话题列表,
//   list → player → 返回收藏). Every forward navigation pushes (path, scrollY)
//   of the page being left onto a trail; a back link pops the newest entry for
//   its target path. Matched by pathname only: back links rebuild the URL, so
//   their search string can differ from the original (param order, an explicit
//   default sort) while it is the same list. The trail — not a per-path map —
//   is what keeps same-path drill-downs apart (/favorites?group=source →
//   /favorites?channel_id=… → player → back → back).
// - Browser back/forward (POP): the exact position saved for that history entry.
//
// Positions are read from scroll events, which are async: when the route-change
// effect runs, lastY still holds the old page's offset, not whatever the new
// page (the player scrolls itself into view) has done since. The restore
// retries each frame until the cached content is tall enough, and gives up on
// a timeout or as soon as the user scrolls themselves.
export function useReturnScroll() {
  const location = useLocation();
  const navType = useNavigationType();
  const prev = useRef({ key: location.key, pathname: location.pathname });
  const lastY = useRef(window.scrollY);
  const trail = useRef<{ path: string; y: number }[]>([]);
  const byKey = useRef(new Map<string, number>());

  useEffect(() => {
    const onScroll = () => {
      lastY.current = window.scrollY;
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  useEffect(() => {
    const from = prev.current;
    prev.current = { key: location.key, pathname: location.pathname };
    if (from.key === location.key) return;

    const y = lastY.current;
    byKey.current.set(from.key, y);

    let target: number | undefined;
    const viaBackLink = (location.state as { restoreScroll?: boolean } | null)?.restoreScroll === true;
    if (viaBackLink) {
      const t = trail.current;
      for (let i = t.length - 1; i >= 0; i--) {
        if (t[i].path === location.pathname) {
          target = t[i].y;
          t.splice(i); // this entry and anything deeper are done with
          break;
        }
      }
    } else if (navType === "POP") {
      target = byKey.current.get(location.key);
    } else if (navType === "PUSH") {
      trail.current.push({ path: from.pathname, y });
      if (trail.current.length > 100) trail.current.shift();
    }
    // REPLACE (a filter flipped in place) neither saves nor restores.

    if (!target || target <= 0) return;
    return restoreTo(target);
  }, [location.key]); // eslint-disable-line react-hooks/exhaustive-deps
}

// restoreTo scrolls to y once the document is tall enough (the list's pages
// come from the query cache but may still be rendering). Returns a cancel.
function restoreTo(y: number): () => void {
  const deadline = performance.now() + 5000;
  let frame = 0;
  let done = false;
  const stop = () => {
    done = true;
    cancelAnimationFrame(frame);
    for (const ev of USER_SCROLL_EVENTS) window.removeEventListener(ev, stop);
  };
  for (const ev of USER_SCROLL_EVENTS) window.addEventListener(ev, stop, { passive: true });

  const tick = () => {
    if (done) return;
    const max = document.documentElement.scrollHeight - window.innerHeight;
    if (max >= y || performance.now() > deadline) {
      window.scrollTo({ top: Math.min(y, Math.max(0, max)) });
      stop();
      return;
    }
    frame = requestAnimationFrame(tick);
  };
  tick();
  return stop;
}

// Any of these means the user is steering the page; a pending restore must not
// yank it back.
const USER_SCROLL_EVENTS = ["wheel", "touchstart", "keydown", "mousedown"] as const;
