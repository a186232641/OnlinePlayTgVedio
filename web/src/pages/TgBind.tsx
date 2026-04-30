import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";

import { api, FlowResp, LoginStage } from "../api/client";

export function TgBind() {
  const [phone, setPhone] = useState("");
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [stage, setStage] = useState<LoginStage>("init");
  const [flowId, setFlowId] = useState<string>("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const nav = useNavigate();
  const qc = useQueryClient();

  const advance = (resp: FlowResp) => {
    setFlowId(resp.flow_id);
    setStage(resp.stage);
    if (resp.stage === "done") {
      qc.invalidateQueries({ queryKey: ["tg"] });
      qc.invalidateQueries({ queryKey: ["index"] });
      nav("/", { replace: true });
    }
  };

  const start = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null); setBusy(true);
    try {
      const resp = await api.post<FlowResp>("/api/tg/login/start", { phone });
      advance(resp);
    } catch (e: any) {
      setErr(e.message ?? "发送验证码失败");
    } finally { setBusy(false); }
  };

  const submitCode = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null); setBusy(true);
    try {
      const resp = await api.post<FlowResp>("/api/tg/login/code", { flow_id: flowId, code });
      advance(resp);
    } catch (e: any) {
      setErr(e.message ?? "验证码错误");
    } finally { setBusy(false); }
  };

  const submitPassword = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null); setBusy(true);
    try {
      const resp = await api.post<FlowResp>("/api/tg/login/password", { flow_id: flowId, password });
      advance(resp);
    } catch (e: any) {
      setErr(e.message ?? "二次验证失败");
    } finally { setBusy(false); }
  };

  return (
    <div className="max-w-md mx-auto p-6 space-y-4">
      <h1 className="text-2xl font-semibold">绑定 Telegram 账号</h1>
      <p className="text-sm text-slate-400">输入手机号(含国码,如 +8613800138000),稍后会在 Telegram 客户端收到验证码。</p>

      {stage === "init" && (
        <form onSubmit={start} className="space-y-3">
          <input className="w-full px-3 py-2 bg-slate-800 rounded" placeholder="+8613800138000"
                 value={phone} onChange={(e) => setPhone(e.target.value)} required />
          <button disabled={busy} className="px-4 py-2 bg-emerald-700 hover:bg-emerald-600 rounded">发送验证码</button>
        </form>
      )}

      {stage === "code_required" && (
        <form onSubmit={submitCode} className="space-y-3">
          <div className="text-sm text-slate-400">已向 {phone} 发送验证码</div>
          <input className="w-full px-3 py-2 bg-slate-800 rounded" placeholder="6 位验证码"
                 value={code} onChange={(e) => setCode(e.target.value)} required />
          <button disabled={busy} className="px-4 py-2 bg-emerald-700 hover:bg-emerald-600 rounded">提交验证码</button>
        </form>
      )}

      {stage === "password_required" && (
        <form onSubmit={submitPassword} className="space-y-3">
          <div className="text-sm text-slate-400">需要二次验证密码 (Two-Step Verification)</div>
          <input className="w-full px-3 py-2 bg-slate-800 rounded" type="password"
                 placeholder="密码"
                 value={password} onChange={(e) => setPassword(e.target.value)} required />
          <button disabled={busy} className="px-4 py-2 bg-emerald-700 hover:bg-emerald-600 rounded">提交密码</button>
        </form>
      )}

      {err && <div className="text-red-400 text-sm">{err}</div>}
    </div>
  );
}
