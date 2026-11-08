"use client";

import { useEffect, useState } from "react";
import { LifeBuoy, LoaderCircle, Save } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { fetchRecoverySettings, updateRecoverySettings, type RecoverySettings } from "@/lib/admin-api";

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

export function RecoverySettingsCard() {
  const [settings, setSettings] = useState<RecoverySettings | null>(null);
  const [domains, setDomains] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void fetchRecoverySettings()
      .then((data) => {
        setSettings(data.settings);
        setDomains((data.settings.outlook_domains || []).join(", "));
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
      });
      setSettings(data.settings);
      setDomains((data.settings.outlook_domains || []).join(", "));
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
      description="后台找回任务的并发、自动自愈策略，以及 outlookEmail 收码渠道（仅用于找回现有账号）。"
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
          <div className="mt-2 font-medium">outlookEmail 渠道</div>
          <Input className={settingsInputClassName} placeholder="base_url，如 https://outlook.example.com" value={settings.outlook_base_url} onChange={(event) => patch("outlook_base_url", event.target.value)} />
          <Input className={settingsInputClassName} placeholder="api_key（X-API-Key）" value={settings.outlook_api_key} onChange={(event) => patch("outlook_api_key", event.target.value)} />
          <label className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">路由到 outlookEmail 的域名（另外 outlook.* / hotmail.* 自动匹配）</span>
            <Input className={settingsInputClassName} value={domains} onChange={(event) => setDomains(event.target.value)} />
          </label>
          <p className="text-xs text-muted-foreground">
            收码路由：域名在某个 CF 临时邮箱配置内 → CF；outlook/hotmail → outlookEmail；其余 → GPTMail。渠道不可用时条目进入“待填码”，不会切换到其他渠道。
          </p>
        </div>
      )}
    </SettingsCard>
  );
}
