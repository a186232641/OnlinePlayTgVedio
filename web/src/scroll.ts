import { useEffect, useRef } from "react";
import { useLocation } from "react-router-dom";

const isPlayer = (path: string) => path.startsWith("/videos/");

// useReturnScroll puts a list page back where it was when the user comes back
// from the player.
//
// Nothing else does: the app uses <BrowserRouter> (no data router, so no
// <ScrollRestoration>), the player's "返回" is a Link that pushes a new entry
// rather than going back, and the player scrolls the window to itself on open.
// So on leaving a page for the player we remember (path, scrollY), and on
// arriving from the player at that same path we scroll there again.
//
// Keyed by pathname only: "返回" rebuilds the list URL from the player's params,
// so the search string can legitimately differ (param order, an explicit
// default sort) while it is the same list.
//
// The list's pages come from the TanStack cache, but may still be rendering
// when we arrive, so the restore retries each frame until the document is tall
// enough, and gives up on a timeout or as soon as the user scrolls themselves.
export function useReturnScroll() {
  const { pathname } = useLocation();
  const prevPath = useRef(pathname);
  // Last scroll position seen by a scroll event. Scroll events are async, so
  // when the route-change effect runs this still holds the list page's offset,
  // not whatever the player's own scrollTo has since done.
  const lastY = useRef(window.scrollY);
  const saved = useRef<{ path: string; y: number } | null>(null);

  useEffect(() => {
    const onScroll = () => {
      lastY.current = window.scrollY;
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  useEffect(() => {
    const prev = prevPath.current;
    prevPath.current = pathname;
    if (prev === pathname) return;

    if (!isPlayer(prev) && isPlayer(pathname)) {
      saved.current = { path: prev, y: lastY.current };
      return;
    }
    if (!isPlayer(prev) || isPlayer(pathname)) return;

    const target = saved.current;
    saved.current = null;
    if (!target || target.path !== pathname || target.y <= 0) return;

    const y = target.y;
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
  }, [pathname]);
}

// Any of these means the user is steering the page; a pending restore must not
// yank it back.
const USER_SCROLL_EVENTS = ["wheel", "touchstart", "keydown", "mousedown"] as const;
