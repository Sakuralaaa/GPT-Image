"use client";

import { useEffect, useState } from "react";
import { LifeBuoy, LoaderCircle, Plug, Plus, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  fetchRecoverySettings,
  testRecoveryCFMail,
  updateRecoverySettings,
  type RecoveryCFMail,
  type RecoverySettings,
} from "@/lib/admin-api";
import { cn } from "@/lib/utils";

import { SettingsCard, settingsInputClassName } from "./settings-ui";

const numberFields: Array<[keyof RecoverySettings, string]> = [
  ["default_concurrency", "手动任务默认并发"],
  ["global_concurrency", "全局并发上限"],
  ["otp_wait_seconds", "自动收码等待（秒）"],
  ["auto_concurrency", "自愈并发"],
  ["auto_interval_minutes", "自愈扫描间隔（分钟）"],
  ["auto_cooldown_minutes", "单账号冷却（分钟，失败指数退避）"],
  ["auto_daily_attempt_cap", "单账号每日尝试上限"],
];

type CFDraft = Omit<RecoveryCFMail, "domains"> & { domains: string; key: string };

const splitDomains = (value: string) =>
  value
    .split(/[\s,;]+/)
    .map((item) => item.trim().replace(/^@+/, "").toLowerCase())
    .filter(Boolean);

const toDrafts = (entries: RecoveryCFMail[] | null | undefined): CFDraft[] =>
  (entries || []).map((entry) => ({ ...entry, domains: (entry.domains || []).join("\n"), key: entry.id }));

let draftSeq = 0;

function Switch({ checked, onChange, label }: { checked: boolean; onChange: (value: boolean) => void; label: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={cn(
        "relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border transition-colors",
        checked ? "border-primary bg-primary" : "border-input bg-transparent",
      )}
    >
      <span className={cn("block size-3.5 rounded-full transition-transform", checked ? "translate-x-[18px] bg-primary-foreground" : "translate-x-[2px] bg-muted-foreground/60")} />
    </button>
  );
}

function CloudflareMailsEditor({ drafts, onChange }: { drafts: CFDraft[]; onChange: (next: CFDraft[]) => void }) {
  const [testing, setTesting] = useState("");
  const update = (key: string, patch: Partial<CFDraft>) => onChange(drafts.map((item) => (item.key === key ? { ...item, ...patch } : item)));
  const add = () =>
    onChange([...drafts, { key: `new-${++draftSeq}`, id: "", name: "", base_url: "", admin_password: "", domains: "", enabled: true }]);
  const remove = (key: string) => onChange(drafts.filter((item) => item.key !== key));
  const test = async (item: CFDraft) => {
    setTesting(item.key);
    try {
      const result = await testRecoveryCFMail({ id: item.id, base_url: item.base_url, admin_password: item.admin_password });
      if (result.ok) toast.success(`「${item.name || item.base_url}」连接成功${typeof result.count === "number" ? `，共 ${result.count} 封邮件` : ""}`);
      else toast.error(result.error || "连接失败");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "连接失败");
    } finally {
      setTesting("");
    }
  };

  return (
    <div className="flex flex-col">
      <div className="mt-4 flex items-center justify-between border-b border-rule-strong pb-2">
        <div className="font-medium">Cloudflare 临时邮箱</div>
        <Button variant="ghost" size="sm" type="button" onClick={add}>
          <Plus className="size-4" />
          添加
        </Button>
      </div>
      <p className="pt-2 text-xs leading-5 text-muted-foreground">
        找回专用，独立于注册机配置。每个部署填写它负责的邮箱后缀（如 mail-a.example，或 *.example.com 匹配子域名）；同一后缀只能属于一个部署。读取优先用 admin 密码（/admin/mails），否则用账号的 mail_token。
      </p>
      {drafts.length === 0 ? <div className="border-b border-border py-4 text-sm text-muted-foreground">暂无配置。未覆盖的域名会沿用注册机的 cloudflare_temp_email 配置。</div> : null}
      {drafts.map((item) => (
        <div key={item.key} className="grid gap-3 border-b border-border py-4 md:grid-cols-2">
          <label className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">名称</span>
            <Input className={settingsInputClassName} placeholder="如 CF 主站" value={item.name} onChange={(event) => update(item.key, { name: event.target.value })} />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">Base URL（worker API 地址）</span>
            <Input className={settingsInputClassName} placeholder="https://mail-api.example.com" value={item.base_url} onChange={(event) => update(item.key, { base_url: event.target.value })} />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">admin 密码（x-admin-auth）</span>
            <Input className={settingsInputClassName} type="password" autoComplete="new-password" placeholder="未设置" value={item.admin_password} onChange={(event) => update(item.key, { admin_password: event.target.value })} />
          </label>
          <label className="flex flex-col gap-1 md:row-span-2">
            <span className="text-xs text-muted-foreground">邮箱后缀（逗号或换行分隔）</span>
            <Textarea className="min-h-[84px] rounded-md py-2 font-mono text-[13px]" placeholder={"mail-a.example\n*.example.com"} value={item.domains} onChange={(event) => update(item.key, { domains: event.target.value })} />
          </label>
          <div className="flex h-9 items-center gap-3 self-end">
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={item.enabled} onChange={(value) => update(item.key, { enabled: value })} label="启用" />
              启用
            </label>
            <div className="ml-auto flex items-center gap-2">
              <Button variant="outline" size="sm" type="button" disabled={testing !== "" || !item.base_url || !item.admin_password} onClick={() => void test(item)}>
                {testing === item.key ? <LoaderCircle className="size-4 animate-spin" /> : <Plug className="size-4" />}
                测试连接
              </Button>
              <Button variant="ghost" size="icon" type="button" title="删除" onClick={() => remove(item.key)}>
                <Trash2 className="size-4" />
              </Button>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

export function RecoverySettingsCard() {
  const [settings, setSettings] = useState<RecoverySettings | null>(null);
  const [domains, setDomains] = useState("");
  const [cfDrafts, setCFDrafts] = useState<CFDraft[]>([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void fetchRecoverySettings()
      .then((data) => {
        setSettings(data.settings);
        setDomains((data.settings.outlook_domains || []).join(", "));
        setCFDrafts(toDrafts(data.settings.cloudflare_mails));
      })
      .catch((error) => toast.error(error instanceof Error ? error.message : "读取找回设置失败"));
  }, []);

  const patch = (key: keyof RecoverySettings, value: unknown) => setSettings((prev) => (prev ? { ...prev, [key]: value } : prev));

  const save = async () => {
    if (!settings) return;
    setSaving(true);
    try {
      const data = await updateRecoverySettings({
        ...settings,
        outlook_domains: domains.split(/[\s,;]+/).map((item) => item.trim()).filter(Boolean),
        cloudflare_mails: cfDrafts.map((item) => ({ id: item.id, name: item.name, base_url: item.base_url, admin_password: item.admin_password, enabled: item.enabled, domains: splitDomains(item.domains) })),
      });
      setSettings(data.settings);
      setDomains((data.settings.outlook_domains || []).join(", "));
      setCFDrafts(toDrafts(data.settings.cloudflare_mails));
      toast.success("找回设置已保存");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "保存失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <SettingsCard
      icon={LifeBuoy}
      title="找回与自愈"
      description="后台找回任务的并发、自动自愈策略，以及找回专用的收码渠道（Cloudflare 临时邮箱、outlookEmail）。"
      tone="amber"
      action={
        <Button size="sm" onClick={() => void save()} disabled={saving || !settings}>
          {saving ? <LoaderCircle className="size-4 animate-spin" /> : <Save className="size-4" />}
          保存
        </Button>
      }
    >
      {!settings ? (
        <LoaderCircle className="size-4 animate-spin text-muted-foreground" />
      ) : (
        <div className="flex flex-col gap-3 text-sm">
          <label className="flex items-center gap-2">
            <Checkbox checked={settings.auto_enabled} onCheckedChange={(value) => patch("auto_enabled", Boolean(value))} />
            启用自动自愈（定期把“异常”账号加入找回队列）
          </label>
          <label className="flex items-center gap-2">
            <Checkbox checked={settings.auto_include_expired} onCheckedChange={(value) => patch("auto_include_expired", Boolean(value))} />
            同时处理“过期待刷新”账号
          </label>
          <label className="flex items-center gap-2">
            <Checkbox checked={settings.auto_skip_without_creds} onCheckedChange={(value) => patch("auto_skip_without_creds", Boolean(value))} />
            跳过既无密码/会话也无可用收码渠道的账号
          </label>
          <div className="grid grid-cols-2 gap-3">
            {numberFields.map(([key, label]) => (
              <label key={key} className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">{label}</span>
                <Input
                  type="number"
                  min={1}
                  className={settingsInputClassName}
                  value={String(settings[key] ?? "")}
                  onChange={(event) => patch(key, Number(event.target.value))}
                />
              </label>
            ))}
          </div>
          <CloudflareMailsEditor drafts={cfDrafts} onChange={setCFDrafts} />
          <div className="mt-4 border-b border-rule-strong pb-2 font-medium">outlookEmail 渠道（Outlook 域名）</div>
          <Input className={settingsInputClassName} placeholder="base_url，如 https://outlook.example.com" value={settings.outlook_base_url} onChange={(event) => patch("outlook_base_url", event.target.value)} />
          <Input className={settingsInputClassName} placeholder="api_key（X-API-Key）" value={settings.outlook_api_key} onChange={(event) => patch("outlook_api_key", event.target.value)} />
          <label className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">Outlook 域名：路由到 outlookEmail 的域名（另外 outlook.* / hotmail.* 自动匹配）</span>
            <Input className={settingsInputClassName} value={domains} onChange={(event) => setDomains(event.target.value)} />
          </label>
          <p className="text-xs text-muted-foreground">
            收码路由：账号手动指定 → 找回 Cloudflare 临时邮箱（按后缀）→ 注册机 CF 配置（沿用）→ Outlook 域名 → GPTMail。渠道不可用时条目进入“待填码”，不会切换到其他渠道。
          </p>
        </div>
      )}
    </SettingsCard>
  );
}
