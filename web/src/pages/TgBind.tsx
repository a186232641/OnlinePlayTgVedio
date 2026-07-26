import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";

import { api, FlowResp, LoginStage } from "../api/client";
import { AlertStrip, Card, PageHeader } from "../components/ui";

// The three login stages, rendered as a stepper so the user can see where they
// are in the phone → code → 2FA flow.
const STEPS: { stage: LoginStage; label: string }[] = [
  { stage: "init", label: "手机号" },
  { stage: "code_required", label: "验证码" },
  { stage: "password_required", label: "二次验证" },
];

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
      qc.invalidateQueries({ queryKey: ["sessions"] });
      qc.invalidateQueries({ queryKey: ["channels"] });
      nav("/tg/accounts", { replace: true });
    }
  };

  const step = async (e: React.FormEvent, path: string, body: object, fallback: string) => {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      advance(await api.post<FlowResp>(path, body));
    } catch (e: any) {
      setErr(e.message ?? fallback);
    } finally {
      setBusy(false);
    }
  };

  const activeIdx = STEPS.findIndex((s) => s.stage === stage);

  return (
    <div className="mx-auto w-full max-w-[560px] space-y-5 p-4 md:p-6">
      <PageHeader
        title="绑定 Telegram 账号"
        meta="输入手机号(含国码,如 +8613800138000),稍后会在 Telegram 客户端收到验证码。"
      />

      <Card className="space-y-6">
        <ol className="flex items-center gap-2">
          {STEPS.map((s, i) => {
            const done = activeIdx > i;
            const active = activeIdx === i;
            return (
              <li key={s.stage} className="flex flex-1 items-center gap-2">
                <span
                  className={
                    "flex size-7 shrink-0 items-center justify-center rounded-full text-theme-xs font-semibold " +
                    (done
                      ? "bg-success-50 text-success-600 dark:bg-success-500/[0.15] dark:text-success-400"
                      : active
                        ? "bg-brand-500 text-white"
                        : "bg-gray-100 text-gray-400 dark:bg-white/[0.06] dark:text-gray-500")
                  }
                >
                  {done ? "✓" : i + 1}
                </span>
                <span
                  className={
                    "truncate text-theme-sm " +
                    (active
                      ? "font-medium text-gray-800 dark:text-white/90"
                      : "text-gray-500 dark:text-gray-400")
                  }
                >
                  {s.label}
                </span>
                {i < STEPS.length - 1 && (
                  <span className="h-px flex-1 bg-gray-200 dark:bg-gray-800" />
                )}
              </li>
            );
          })}
        </ol>

        {stage === "init" && (
          <form
            onSubmit={(e) => step(e, "/api/tg/login/start", { phone }, "发送验证码失败")}
            className="flex flex-col gap-4"
          >
            <label className="flex flex-col gap-1.5">
              <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">
                手机号
              </span>
              <input
                className="field"
                placeholder="+8613800138000"
                value={phone}
                onChange={(e) => setPhone(e.target.value)}
                required
              />
            </label>
            <button disabled={busy} className="btn btn-primary self-start">
              {busy ? "发送中…" : "发送验证码"}
            </button>
          </form>
        )}

        {stage === "code_required" && (
          <form
            onSubmit={(e) =>
              step(e, "/api/tg/login/code", { flow_id: flowId, code }, "验证码错误")
            }
            className="flex flex-col gap-4"
          >
            <div className="text-theme-sm text-gray-500 dark:text-gray-400">
              已向 <span className="font-medium text-gray-700 dark:text-gray-300">{phone}</span> 发送验证码
            </div>
            <label className="flex flex-col gap-1.5">
              <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">
                验证码
              </span>
              <input
                className="field tracking-[0.3em]"
                placeholder="6 位验证码"
                inputMode="numeric"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                required
              />
            </label>
            <button disabled={busy} className="btn btn-primary self-start">
              {busy ? "提交中…" : "提交验证码"}
            </button>
          </form>
        )}

        {stage === "password_required" && (
          <form
            onSubmit={(e) =>
              step(e, "/api/tg/login/password", { flow_id: flowId, password }, "二次验证失败")
            }
            className="flex flex-col gap-4"
          >
            <div className="text-theme-sm text-gray-500 dark:text-gray-400">
              该账号开启了两步验证 (Two-Step Verification)
            </div>
            <label className="flex flex-col gap-1.5">
              <span className="text-theme-xs font-medium text-gray-500 dark:text-gray-400">
                二次验证密码
              </span>
              <input
                className="field"
                type="password"
                placeholder="密码"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </label>
            <button disabled={busy} className="btn btn-primary self-start">
              {busy ? "提交中…" : "提交密码"}
            </button>
          </form>
        )}

        {err && <AlertStrip title={err} />}
      </Card>
    </div>
  );
}
