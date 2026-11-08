"use client";

import { useEffect, useState } from "react";
import { ImageIcon, LoaderCircle, Save } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { httpRequest } from "@/lib/request";

import { SettingsCard, settingsInputClassName } from "./settings-ui";

type CatalogEntry = { id: string; label: string; alias_of?: string; default_upstream?: string; description?: string };

export function ImageModelUpstreamCard() {
  const [catalog, setCatalog] = useState<CatalogEntry[]>([]);
  const [values, setValues] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void (async () => {
      try {
        const data = await httpRequest<{ config: { image_model_catalog?: CatalogEntry[]; image_model_upstream?: Record<string, string> } }>("/api/settings");
        setCatalog((data.config.image_model_catalog || []).filter((entry) => !entry.alias_of));
        setValues(data.config.image_model_upstream || {});
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "读取模型映射失败");
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  const save = async () => {
    setSaving(true);
    try {
      await httpRequest("/api/settings", { method: "POST", body: { image_model_upstream: values } });
      toast.success("模型映射已保存");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "保存失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <SettingsCard
      icon={ImageIcon}
      title="图像模型映射"
      description="网页通道没有按请求指定的图像模型，由驱动的对话模型决定。上游模型失效时可在这里改。"
      tone="violet"
      action={
        <Button size="sm" onClick={() => void save()} disabled={saving || loading}>
          {saving ? <LoaderCircle className="size-4 animate-spin" /> : <Save className="size-4" />}
          保存
        </Button>
      }
    >
      {loading ? (
        <LoaderCircle className="size-4 animate-spin text-muted-foreground" />
      ) : (
        <div className="flex flex-col gap-3">
          {catalog.map((entry) => (
            <label key={entry.id} className="flex flex-col gap-1 text-sm">
              <span className="font-medium">
                {entry.label} <span className="font-mono text-xs text-muted-foreground">{entry.id}</span>
              </span>
              <Input
                className={settingsInputClassName}
                value={values[entry.id] ?? ""}
                placeholder={entry.default_upstream || "auto"}
                onChange={(event) => setValues((prev) => ({ ...prev, [entry.id]: event.target.value }))}
              />
              {entry.description ? <span className="text-xs text-muted-foreground">默认 {entry.default_upstream} · {entry.description}</span> : null}
            </label>
          ))}
          <p className="text-xs text-muted-foreground">gpt-image-2 / 2.5 / codex-gpt-image-2 等旧模型名作为别名映射到对应的 2.6 条目。</p>
        </div>
      )}
    </SettingsCard>
  );
}
