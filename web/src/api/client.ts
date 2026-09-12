// Thin fetch wrapper. All requests go to /api/* on the same origin so cookies
// (the JWT session) are sent automatically.

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method,
    credentials: "include",
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  };
  const res = await fetch(path, init);
  if (!res.ok) {
    let payload: any = null;
    try { payload = await res.json(); } catch { /* ignore */ }
    throw new ApiError(res.status, payload?.code ?? "error", payload?.message ?? res.statusText);
  }
  if (res.status === 204) return undefined as T;
  const ct = res.headers.get("content-type") ?? "";
  if (!ct.startsWith("application/json")) return undefined as T;
  return res.json() as Promise<T>;
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body),
  patch: <T>(path: string, body?: unknown) => request<T>("PATCH", path, body),
  del: <T>(path: string) => request<T>("DELETE", path),
};

// --- Types ---

export interface Channel {
  id: number;
  tg_session_id: number;
  tg_channel_id: number;
  title: string;
  username?: string;
  video_count: number;
  photo_count: number;
  last_indexed_at?: string;
  group_by_streamer: boolean;
  auto_sync: boolean;

  // Forum shape. `dialog_kind` is "channel" | "megagroup" | "topic";
  // `is_forum` marks a group whose content lives in topics, and a topic row
  // carries `topic_id` + `parent_channel_id`. A topic IS a channel row, so the
  // channel detail route renders both.
  dialog_kind: "channel" | "megagroup" | "topic";
  is_forum: boolean;
  topic_count: number;
  topic_id?: number;
  parent_channel_id?: number;
  topic_closed?: boolean;
  topics_synced_at?: string;
}

export interface Streamer {
  streamer: string; // "" = filenames not matching the {streamer}-DATE pattern
  count: number;
}

// Video field names mirror TG Desktop's JSON export (snake_case as-is).
export interface Video {
  id: number;
  channel_id: number;
  tg_msg_id: number;
  date?: string;
  from?: string;
  from_id?: string;
  file_name?: string;
  file_size: number;
  media_type?: string;
  mime_type?: string;
  duration_seconds: number;
  width: number;
  height: number;
  text: string;
  stream_url: string;
}

// MediaItem is one row of a merged video+image list. `kind` decides how it is
// opened: a video navigates to the player, an image opens the lightbox.
export interface MediaItem {
  kind: "video" | "photo";
  id: number;
  channel_id: number;
  tg_msg_id: number;
  date?: string;
  from?: string;
  file_name?: string;
  file_size: number;
  media_type?: string;
  mime_type?: string;
  duration_seconds: number;
  width: number;
  height: number;
  text: string;
  url: string;       // stream URL (video) / full image URL (photo)
  thumb_url: string;
}

// MediaCursor is the two-part keyset cursor of a merged list: videos and images
// are separate tables with separate id sequences, so each side carries its own
// position (see internal/db/media.go).
export interface MediaCursor {
  video?: number;
  photo?: number;
}

export interface MediaPage {
  items: MediaItem[];
  next: MediaCursor;
  has_more: boolean;
  total_videos?: number;
  total_photos?: number;
}

export type MediaKindFilter = "" | "video" | "photo";

// SyncState mirrors indexer.SyncState. `phase` is "syncing" for a plain
// channel/topic and "topics" / "话题 3/12: …" while a forum group fans out.
export interface SyncState {
  running: boolean;
  phase?: string;
  walked: number;
  imported: number;
  videos: number;
  photos: number;
  skipped: number;
  last_error?: string;
  started_at?: string;
  finished_at?: string;
}

// A topic is a channel row; the list endpoint attaches its live sync state so
// the page can poll one URL instead of one per topic.
export interface Topic extends Channel {
  sync?: SyncState;
}

export interface TgSession {
  id: number;
  phone?: string;
  tg_user_id?: number;
  label?: string;
  status: "pending" | "active" | "revoked";
  discover_status?: "idle" | "running" | "done" | "failed";
  discover_error?: string;
}

export interface Me {
  user_id: number;
  email: string;
}

export type LoginStage = "init" | "code_required" | "password_required" | "done" | "error";

export interface FlowResp {
  flow_id: string;
  stage: LoginStage;
}
