import { Navigate, Route, Routes } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, Me } from "./api/client";
import { Layout } from "./components/Layout";
import { Login } from "./pages/Login";
import { Register } from "./pages/Register";
import { TgBind } from "./pages/TgBind";
import { Channels } from "./pages/Channels";
import { ChannelDetail } from "./pages/ChannelDetail";
import { Player } from "./pages/Player";
import { Favorites } from "./pages/Favorites";
import { Search } from "./pages/Search";

function useMe() {
  return useQuery<Me | null>({
    queryKey: ["me"],
    queryFn: async () => {
      try { return await api.get<Me>("/api/auth/me"); }
      catch { return null; }
    },
  });
}

function Private({ children }: { children: JSX.Element }) {
  const me = useMe();
  if (me.isLoading) return <div className="p-8 text-slate-400">加载中…</div>;
  if (!me.data) return <Navigate to="/login" replace />;
  return children;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/register" element={<Register />} />
      <Route element={<Private><Layout /></Private>}>
        <Route path="/" element={<Channels />} />
        <Route path="/channels/:id" element={<ChannelDetail />} />
        <Route path="/videos/:id" element={<Player />} />
        <Route path="/favorites" element={<Favorites />} />
        <Route path="/search" element={<Search />} />
        <Route path="/tg/bind" element={<TgBind />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
