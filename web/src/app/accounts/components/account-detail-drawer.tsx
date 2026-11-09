import { useCallback, useEffect, useState } from "react";
import { Copy, Eye, EyeOff, KeyRound, LoaderCircle, Mail, RefreshCw, Save, Send, X } from "lucide-react";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";

import { formatTime } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  MAIL_CHANNEL_LABELS,
  fetchAccountDetail,
  readAccountLatestMail,
  refreshAccountToken,
  replaceAccountToken,
  startAccountLogin,
  updateAccountCredentials,
  type AccountCredentialUpdate,
  type AccountDetail,
  type LatestMail,
} from "@/lib/admin-api";

type Props = {
  accountId: string | null;
  onClose: () => void;
  onChanged: (nextId?: string) => void;
};

const editableFields: Array<[keyof AccountCredentialUpdate, string, boolean]> = [
  ["email", "邮箱", false],
  ["password", "密码", true],
  ["mail_provider", "邮箱提供商 (mail_provider)", false],
  ["mail_ref", "邮箱配置引用 (mail_ref)", false],
  ["mail_token", "邮箱凭证 (mail_token / JWT)", true],
];

function copy(text: string) {
  if (!text) return;
  void navigator.clipboard.writeText(text).then(() => toast.success("已复制"));
}

export function AccountDetailDrawer({ accountId, onClose, onChanged }: Props) {
  const navigate = useNavigate();
  const [detail, setDetail] = useState<AccountDetail | null>(null);
  const [form, setForm] = useState<AccountCredentialUpdate>({});
  const [revealed, setRevealed] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState("");
  const [mail, setMail] = useState<LatestMail | null>(null);
  const [tokenInput, setTokenInput] = useState("");

  const applyDetail = useCallback((item: AccountDetail) => {
    setDetail(item);
    setForm({
      email: item.email,
      password: item.password,
      mail_provider: item.mail_provider,
      mail_ref: item.mail_ref,
      mail_token: item.mail_token,
      mail_channel: item.mail_channel,
      note: item.note,
    });
  }, []);

  useEffect(() => {
    if (!accountId) return;
    void fetchAccountDetail(accountId)
      .then((data) => applyDetail(data.item))
      .catch((error) => toast.error(error instanceof Error ? error.message : "加载账号失败"));
  }, [accountId, applyDetail]);

  if (!accountId) return null;

  const act = async (key: string, fn: () => Promise<void>) => {
    setBusy(key);
    try {
      await fn();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "操作失败");
    } finally {
      setBusy("");
    }
  };

  const save = () =>
    act("save", async () => {
      const data = await updateAccountCredentials(accountId, form);
      applyDetail(data.item);
      toast.success("已保存");
      onChanged();
    });

  const tokenChanged = (item: AccountDetail) => {
    applyDetail(item);
    onChanged(item.id);
  };

  const route = detail?.mail_route;

  return (
    <div className="fixed inset-0 z-50">
      <div className="absolute inset-0 bg-black/40" onClick={onClose} />
      <div className="absolute inset-y-0 right-0 flex w-full max-w-xl flex-col bg-background shadow-2xl">
        <div className="flex items-center justify-between border-b border-border/60 px-5 py-3">
          <div className="min-w-0">
            <div className="truncate font-semibold">{detail?.email || "账号详情"}</div>
            <div className="font-mono text-xs text-muted-foreground">{detail?.token_preview}</div>
          </div>
          <button type="button" onClick={onClose} className="rounded-lg p-1.5 hover:bg-muted" aria-label="关闭">
            <X className="size-4" />
          </button>
        </div>
        {!detail ? (
          <div className="flex flex-1 items-center justify-center">
            <LoaderCircle className="size-5 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <div className="flex-1 space-y-5 overflow-y-auto px-5 py-4 text-sm">
            <section className="grid grid-cols-2 gap-2 text-xs">
              <div><span className="text-muted-foreground">状态</span> <Badge variant="outline">{detail.status}</Badge> <Badge variant="secondary">{detail.type}</Badge></div>
              <div><span className="text-muted-foreground">入库</span> {formatTime(detail.created_at)}</div>
              <div><span className="text-muted-foreground">最近刷新</span> {formatTime(detail.last_refresh_at)}</div>
              <div><span className="text-muted-foreground">刷新/重登失败</span> {detail.refresh_failures} / {detail.relogin_failures}</div>
              {detail.last_error ? (
                <div className="col-span-2 rounded-lg bg-rose-50 px-3 py-2 text-rose-700 dark:bg-rose-950/30 dark:text-rose-300">
                  失败原因：{detail.last_error}
                </div>
              ) : null}
            </section>

            <section className="space-y-3">
              <h3 className="font-semibold">注册凭证</h3>
              {editableFields.map(([field, label, secret]) => (
                <label key={field} className="flex flex-col gap-1">
                  <span className="text-xs text-muted-foreground">{label}</span>
                  <div className="flex gap-1">
                    <Input
                      type={secret && !revealed[field] ? "password" : "text"}
                      className="font-mono text-xs"
                      value={String(form[field] ?? "")}
                      onChange={(event) => setForm({ ...form, [field]: event.target.value })}
                    />
                    {secret ? (
                      <Button variant="outline" size="icon" type="button" onClick={() => setRevealed((prev) => ({ ...prev, [field]: !prev[field] }))} title="显示/隐藏">
                        {revealed[field] ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                      </Button>
                    ) : null}
                    <Button variant="outline" size="icon" type="button" onClick={() => copy(String(form[field] ?? ""))} title="复制">
                      <Copy className="size-4" />
                    </Button>
                  </div>
                </label>
              ))}
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">
                  收码渠道（当前路由：{route ? `${MAIL_CHANNEL_LABELS[route.channel] || route.channel}${route.entry_name || route.entry_ref ? ` · ${route.entry_name || route.entry_ref}` : ""}${route.ready ? (route.reason ? ` · ${route.reason}` : "") : ` · 不可用：${route.reason}`}` : "—"}）
                </span>
                <select
                  className="h-9 rounded-lg border border-input bg-background px-2 text-sm"
                  value={form.mail_channel || ""}
                  onChange={(event) => setForm({ ...form, mail_channel: event.target.value })}
                >
                  <option value="">自动（按域名路由）</option>
                  <option value="cloudflare_temp_email">CF 临时邮箱</option>
                  <option value="outlook">outlookEmail</option>
                  <option value="gptmail">GPTMail</option>
                </select>
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">备注</span>
                <Input value={form.note || ""} onChange={(event) => setForm({ ...form, note: event.target.value })} />
              </label>
              <Button onClick={() => void save()} disabled={busy !== ""}>
                {busy === "save" ? <LoaderCircle className="size-4 animate-spin" /> : <Save className="size-4" />}
                保存凭证
              </Button>
            </section>

            <section className="space-y-2">
              <h3 className="font-semibold">邮箱</h3>
              <Button variant="outline" size="sm" disabled={busy !== ""} onClick={() => void act("mail", async () => {
                const data = await readAccountLatestMail(accountId);
                setMail(data.mail);
              })}>
                {busy === "mail" ? <LoaderCircle className="size-4 animate-spin" /> : <Mail className="size-4" />}
                读取最新邮件 / 验证码
              </Button>
              {mail ? (
                <div className="rounded-lg border border-border/70 p-3 text-xs">
                  <div className="text-muted-foreground">渠道 {MAIL_CHANNEL_LABELS[mail.channel] || mail.channel}{mail.entry_name ? ` · ${mail.entry_name}` : ""} {mail.received_at ? `· ${formatTime(mail.received_at)}` : ""}</div>
                  {mail.empty ? (
                    <div className="mt-1">收件箱为空</div>
                  ) : (
                    <>
                      <div className="mt-1 font-medium">{mail.subject}</div>
                      {mail.code ? (
                        <button type="button" className="mt-2 rounded-lg bg-primary/10 px-3 py-1 font-mono text-lg font-semibold text-primary" onClick={() => copy(mail.code || "")}>
                          {mail.code}
                        </button>
                      ) : null}
                      <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-words text-muted-foreground">{mail.text}</pre>
                    </>
                  )}
                </div>
              ) : null}
            </section>

            <section className="space-y-2">
              <h3 className="font-semibold">手动操作</h3>
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" disabled={busy !== ""} onClick={() => void act("refresh", async () => {
                  const data = await refreshAccountToken(accountId);
                  tokenChanged(data.item);
                  toast.success("Token 已刷新");
                })}>
                  {busy === "refresh" ? <LoaderCircle className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
                  刷新 Token
                </Button>
                <Button variant="outline" size="sm" disabled={busy !== "" || !detail.password} onClick={() => void act("relogin", async () => {
                  await startAccountLogin(accountId, "relogin");
                  toast.success("已加入找回任务（密码登录）");
                  navigate("/recovery");
                })}>
                  <KeyRound className="size-4" />
                  密码重登
                </Button>
                <Button variant="outline" size="sm" disabled={busy !== ""} onClick={() => void act("otp", async () => {
                  await startAccountLogin(accountId, "otp-login");
                  toast.success("已加入找回任务（验证码登录），可在找回任务页填码");
                  navigate("/recovery");
                })}>
                  <Send className="size-4" />
                  邮箱验证码登录
                </Button>
              </div>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">粘贴 access_token 或 /api/auth/session JSON 覆盖当前 Token</span>
                <Textarea className="min-h-20 font-mono text-xs" value={tokenInput} onChange={(event) => setTokenInput(event.target.value)} />
              </label>
              <Button variant="outline" size="sm" disabled={busy !== "" || !tokenInput.trim()} onClick={() => void act("token", async () => {
                const text = tokenInput.trim();
                const data = await replaceAccountToken(accountId, text.startsWith("{") ? { session_json: text } : { access_token: text });
                tokenChanged(data.item);
                setTokenInput("");
                toast.success("Token 已覆盖");
              })}>
                {busy === "token" ? <LoaderCircle className="size-4 animate-spin" /> : <Save className="size-4" />}
                覆盖 Token
              </Button>
            </section>
          </div>
        )}
      </div>
    </div>
  );
}
