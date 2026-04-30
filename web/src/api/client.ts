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
  del: <T>(path: string) => request<T>("DELETE", path),
};

// --- Types ---

export interface Channel {
  id: number;
  tg_channel_id: number;
  title: string;
  username?: string;
  video_count: number;
  last_indexed_at?: string;
}

export interface Video {
  id: number;
  channel_id: number;
  caption: string;
  duration_sec: number;
  width: number;
  height: number;
  size_bytes: number;
  mime: string;
  sent_at?: string;
  thumb_url?: string;
  stream_url: string;
}

export interface IndexStatus {
  status: "idle" | "running" | "done" | "failed";
  channels_total?: number;
  channels_done?: number;
  videos_found?: number;
  last_error?: string;
}

export interface TgStatus {
  bound: boolean;
  phone?: string;
  tg_user_id?: number;
  status?: string;
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
