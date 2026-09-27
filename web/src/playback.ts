import mpegts from "mpegts.js";

// attachStream sets a <video> up to play one stream URL, picking the playback
// path by sniffing the container (the first 16 bytes via a Range request):
// FLV and MPEG-TS go through mpegts.js, everything else (MP4 …) plays natively.
// It starts playback and returns a cleanup that tears everything down; call it
// before attaching the next URL. Shared by the player page and the slideshow.
//
// Two mpegts.js behaviours matter here, both read from its source:
// - accurateSeek defaults to false, so a seek into an unbuffered range lands on
//   the nearest keyframe (up to a whole GOP off) — we turn it on. It's the
//   createPlayer SECOND argument (Config), not the media data source.
// - It can only seek outside the buffered range when the file carries a
//   keyframe index (FLV onMetaData.keyframes; TS never has one):
//   MediaInfo.isSeekable() is literally hasKeyframesIndex. That's a property of
//   the file, reported through onSeekLimited so the UI can say why.
export interface StreamHandlers {
  onHint?: (hint: string) => void; // container label for a badge
  onError?: (msg: string, diag?: string) => void;
  onSeekLimited?: () => void;
  // If the browser blocks autoplay with sound, start muted instead of staying
  // paused (the slideshow's auto mode would otherwise stall on that video).
  retryMuted?: boolean;
}

async function playOrMute(video: HTMLVideoElement, play: () => Promise<void> | void, retryMuted?: boolean) {
  try {
    await play();
  } catch (e) {
    if (!retryMuted || (e as DOMException)?.name !== "NotAllowedError") return;
    video.muted = true;
    try { await play(); } catch { /* give up — the controls are still there */ }
  }
}

export function attachStream(video: HTMLVideoElement, url: string, h: StreamHandlers = {}): () => void {
  let cancelled = false;
  let player: mpegts.Player | null = null;

  (async () => {
    let kind: "flv" | "mpegts" | "native" = "native";
    let hint = "";
    try {
      const r = await fetch(url, { headers: { Range: "bytes=0-15" }, credentials: "include" });
      if (r.ok) {
        const b = new Uint8Array(await r.arrayBuffer());
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
      // probe failed — fall through to native
    }
    if (cancelled) return;
    h.onHint?.(hint);

    if (kind === "flv" || kind === "mpegts") {
      if (!mpegts.getFeatureList().mseLivePlayback) {
        h.onError?.("浏览器不支持 MSE — 该视频(FLV/TS)无法播放");
        return;
      }
      player = mpegts.createPlayer(
        { type: kind, url, isLive: false, cors: true, withCredentials: true },
        { accurateSeek: true },
      );
      player.on(mpegts.Events.MEDIA_INFO, (info: { hasKeyframesIndex?: boolean | null }) => {
        if (!cancelled && info?.hasKeyframesIndex !== true) h.onSeekLimited?.();
      });
      player.attachMediaElement(video);
      player.on(mpegts.Events.ERROR, (errType, errDetail, errInfo) => {
        h.onError?.(`mpegts ${errType}: ${errDetail}`, JSON.stringify(errInfo));
      });
      player.load();
      const p = player;
      await playOrMute(video, () => p.play() as Promise<void> | void, h.retryMuted);
    } else {
      video.src = url;
      await playOrMute(video, () => video.play(), h.retryMuted);
    }
  })();

  return () => {
    cancelled = true;
    if (player) {
      try { player.destroy(); } catch { /* noop */ }
      player = null;
    }
    try {
      video.pause();
      video.removeAttribute("src");
      video.load();
    } catch { /* noop */ }
  };
}
