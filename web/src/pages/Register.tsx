import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";

import { api } from "../api/client";
import { AuthShell } from "./AuthShell";

export function Register() {
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
      await api.post("/api/auth/register", { email, password });
      await qc.invalidateQueries({ queryKey: ["me"] });
      nav("/tg/bind", { replace: true });
    } catch (e: any) {
      setErr(e.message ?? "注册失败");
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthShell
      title="注册"
      subtitle="创建 Web 账号后,下一步绑定你的 Telegram 账号。"
      error={err}
      onSubmit={submit}
      footer={
        <>
          已有账号？
          <Link to="/login" className="font-medium text-brand-500 hover:text-brand-600">
            登录
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
        <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">
          密码 (至少 8 位)
        </span>
        <input
          className="field"
          type="password"
          placeholder="••••••••"
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          minLength={8}
          required
        />
      </label>
      <button disabled={busy} className="btn btn-primary w-full">
        {busy ? "创建中…" : "创建账号"}
      </button>
    </AuthShell>
  );
}
