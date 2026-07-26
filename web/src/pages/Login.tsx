import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";

import { api } from "../api/client";
import { AuthShell } from "./AuthShell";

export function Login() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const nav = useNavigate();
  const qc = useQueryClient();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      await api.post("/api/auth/login", { email, password });
      await qc.invalidateQueries({ queryKey: ["me"] });
      nav("/", { replace: true });
    } catch (e: any) {
      setErr(e.message ?? "登录失败");
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthShell
      title="登录"
      subtitle="登录后可浏览、搜索并在线播放已索引的 Telegram 频道视频。"
      error={err}
      onSubmit={submit}
      footer={
        <>
          还没有账号？
          <Link to="/register" className="font-medium text-brand-500 hover:text-brand-600">
            注册
          </Link>
        </>
      }
    >
      <label className="flex flex-col gap-1.5">
        <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">邮箱</span>
        <input
          className="field"
          type="email"
          placeholder="you@example.com"
          autoComplete="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
        />
      </label>
      <label className="flex flex-col gap-1.5">
        <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">密码</span>
        <input
          className="field"
          type="password"
          placeholder="••••••••"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
      </label>
      <button disabled={busy} className="btn btn-primary w-full">
        {busy ? "登录中…" : "登录"}
      </button>
    </AuthShell>
  );
}
