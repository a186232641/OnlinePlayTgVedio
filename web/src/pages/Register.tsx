import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";

import { api } from "../api/client";

export function Register() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const nav = useNavigate();
  const qc = useQueryClient();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    try {
      await api.post("/api/auth/register", { email, password });
      await qc.invalidateQueries({ queryKey: ["me"] });
      nav("/tg/bind", { replace: true });
    } catch (e: any) {
      setErr(e.message ?? "注册失败");
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center">
      <form onSubmit={submit} className="w-80 space-y-4 p-6 rounded bg-slate-900 border border-slate-800">
        <h1 className="text-xl font-semibold">注册</h1>
        <input className="w-full px-3 py-2 bg-slate-800 rounded" type="email" placeholder="邮箱"
               value={email} onChange={(e) => setEmail(e.target.value)} required />
        <input className="w-full px-3 py-2 bg-slate-800 rounded" type="password" placeholder="密码 (至少 8 位)"
               value={password} onChange={(e) => setPassword(e.target.value)} minLength={8} required />
        {err && <div className="text-red-400 text-sm">{err}</div>}
        <button className="w-full py-2 bg-emerald-700 hover:bg-emerald-600 rounded">创建账号</button>
        <div className="text-sm text-slate-400">
          已有账号？<Link to="/login" className="text-emerald-400">登录</Link>
        </div>
      </form>
    </div>
  );
}
