import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { api, Video } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";

export function Search() {
  const [q, setQ] = useState("");
  const [submitted, setSubmitted] = useState("");
  const r = useQuery<{ videos: Video[] }>({
    queryKey: ["search", submitted],
    queryFn: () => api.get(`/api/videos/search?q=${encodeURIComponent(submitted)}`),
    enabled: submitted.length > 0,
  });
  return (
    <div>
      <form onSubmit={(e) => { e.preventDefault(); setSubmitted(q.trim()); }}
            className="p-4 border-b border-slate-800 flex gap-2">
        <input className="flex-1 px-3 py-2 bg-slate-800 rounded" placeholder="搜索视频说明…"
               value={q} onChange={(e) => setQ(e.target.value)} />
        <button className="px-4 py-2 bg-emerald-700 hover:bg-emerald-600 rounded">搜索</button>
      </form>
      {submitted && r.data && <VideoGrid videos={r.data.videos} />}
    </div>
  );
}
