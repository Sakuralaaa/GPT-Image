import { useCallback, useEffect, useState } from "react";
import { Copy, LoaderCircle, Pencil, Plus, RotateCcw, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { AdminGuard, StatCard, StatRow } from "@/components/admin-page";
import { formatTime, limitText } from "@/lib/format";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  createApiKey,
  deleteApiKey,
  fetchApiKeys,
  resetApiKeyUsage,
  updateApiKey,
  type ApiKeyInput,
  type ApiKeyItem,
  type UsageSummary,
} from "@/lib/admin-api";

type FormState = {
  name: string;
  key: string;
  enabled: boolean;
  expires_at: string;
  total_limit: string;
  daily_limit: string;
  image_limit: string;
  rpm_limit: string;
  concurrency_limit: string;
  models: string;
};

const emptyForm: FormState = {
  name: "",
  key: "",
  enabled: true,
  expires_at: "",
  total_limit: "0",
  daily_limit: "0",
  image_limit: "0",
  rpm_limit: "0",
  concurrency_limit: "0",
  models: "",
};

function toLocalInput(value: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function formFromKey(item: ApiKeyItem): FormState {
  return {
    name: item.name,
    key: "",
    enabled: item.enabled,
    expires_at: toLocalInput(item.expires_at),
    total_limit: String(item.total_limit),
    daily_limit: String(item.daily_limit),
    image_limit: String(item.image_limit),
    rpm_limit: String(item.rpm_limit),
    concurrency_limit: String(item.concurrency_limit),
    models: (item.models || []).join(", "),
  };
}

function inputFromForm(form: FormState, creating: boolean): ApiKeyInput {
  const input: ApiKeyInput = {
    name: form.name.trim(),
    enabled: form.enabled,
    expires_at: form.expires_at ? new Date(form.expires_at).toISOString() : "",
    total_limit: Number(form.total_limit) || 0,
    daily_limit: Number(form.daily_limit) || 0,
    image_limit: Number(form.image_limit) || 0,
    rpm_limit: Number(form.rpm_limit) || 0,
    concurrency_limit: Number(form.concurrency_limit) || 0,
    models: form.models,
  };
  if (creating && form.key.trim()) {
    input.key = form.key.trim();
  }
  return input;
}

const limitFields: Array<[keyof FormState, string]> = [
  ["total_limit", "总请求次数上限"],
  ["daily_limit", "每日请求次数上限"],
  ["image_limit", "图片数量上限（按 n 计）"],
  ["rpm_limit", "每分钟请求数 RPM"],
  ["concurrency_limit", "并发上限"],
];

function copy(text: string) {
  void navigator.clipboard.writeText(text).then(() => toast.success("已复制"));
}

function KeysContent() {
  const [items, setItems] = useState<ApiKeyItem[]>([]);
  const [summary, setSummary] = useState<UsageSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const [editing, setEditing] = useState<ApiKeyItem | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [createdKey, setCreatedKey] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await fetchApiKeys();
      setItems(data.items);
      setSummary(data.summary);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(timer);
  }, [load]);

  const openCreate = () => {
    setEditing(null);
    setForm(emptyForm);
    setDialogOpen(true);
  };

  const openEdit = (item: ApiKeyItem) => {
    setEditing(item);
    setForm(formFromKey(item));
    setDialogOpen(true);
  };

  const save = async () => {
    setSaving(true);
    try {
      if (editing) {
        await updateApiKey(editing.id, inputFromForm(form, false));
        toast.success("已保存");
      } else {
        const data = await createApiKey(inputFromForm(form, true));
        setCreatedKey(data.key);
      }
      setDialogOpen(false);
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "保存失败");
    } finally {
      setSaving(false);
    }
  };

  const toggle = async (item: ApiKeyItem) => {
    try {
      await updateApiKey(item.id, { enabled: !item.enabled });
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "操作失败");
    }
  };

  const reset = async (item: ApiKeyItem) => {
    if (!window.confirm(`重置「${item.name}」的用量计数？`)) return;
    try {
      await resetApiKeyUsage(item.id);
      toast.success("用量已重置");
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "操作失败");
    }
  };

  const remove = async (item: ApiKeyItem) => {
    if (!window.confirm(`删除「${item.name}」？使用该 Key 的客户端将立即失效。`)) return;
    try {
      await deleteApiKey(item.id);
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除失败");
    }
  };

  return (
    <>
      <PageHeader
        eyebrow="API Keys"
        title="API Key"
        dek="独立于账号的调用凭证：每个 Key 可单独设置额度、RPM、并发、模型白名单与有效期。"
        actions={
          <Button onClick={openCreate}>
            <Plus className="size-4" />
            新建 Key
          </Button>
        }
      />
      <StatRow>
        <StatCard label="Key 总数" value={summary?.keys ?? "—"} unit="个" hint={`可用 ${summary?.enabled_keys ?? 0}`} />
        <StatCard label="今日请求" value={summary?.today_requests ?? "—"} unit="次" />
        <StatCard label="今日出图" value={summary?.today_images ?? "—"} unit="张" />
        <StatCard label="累计请求 / 出图" value={`${summary?.total_requests ?? 0} / ${summary?.total_images ?? 0}`} />
      </StatRow>
      <section className="flex flex-col gap-4 overflow-x-auto">
        <div className="flex h-9 items-center gap-3">
          <h2 className="font-serif text-xl font-medium">Key 列表</h2>
          <span className="num text-sm text-muted-foreground">{items.length}</span>
        </div>
        <Table className="border-t border-rule-strong">
          <TableHeader>
            <TableRow>
              <TableHead>名称 / Key</TableHead>
              <TableHead>状态</TableHead>
              <TableHead className="text-right">今日</TableHead>
              <TableHead className="text-right">累计请求</TableHead>
              <TableHead className="text-right">图片</TableHead>
              <TableHead className="text-right">RPM / 并发</TableHead>
              <TableHead className="pl-8">模型</TableHead>
              <TableHead>最近使用</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading && items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={9} className="py-10 text-center">
                  <LoaderCircle className="mx-auto size-5 animate-spin text-muted-foreground" />
                </TableCell>
              </TableRow>
            ) : null}
            {!loading && items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={9} className="py-6 text-left text-sm text-muted-foreground">
                  还没有 API Key。新建后客户端用 <code>Authorization: Bearer &lt;key&gt;</code> 调用 /v1 接口。
                </TableCell>
              </TableRow>
            ) : null}
            {items.map((item) => (
              <TableRow key={item.id}>
                <TableCell>
                  <div className="font-medium">{item.name}</div>
                  <div className="font-mono text-xs text-muted-foreground">{item.key_prefix}</div>
                  {item.source === "migrated" ? <Badge variant="outline" className="mt-1">迁移</Badge> : null}
                </TableCell>
                <TableCell>
                  <button type="button" onClick={() => void toggle(item)} title="点击切换启用状态">
                    {item.expired ? (
                      <Badge variant="danger">已过期</Badge>
                    ) : item.enabled ? (
                      <Badge variant="success">启用</Badge>
                    ) : (
                      <Badge variant="secondary">停用</Badge>
                    )}
                  </button>
                  {item.expires_at ? <div className="mt-1 text-xs text-muted-foreground">到期 {formatTime(item.expires_at)}</div> : null}
                </TableCell>
                <TableCell className="text-right tabular-nums text-sm">
                  {limitText(item.today_requests, item.daily_limit)}
                  <div className="text-xs text-muted-foreground">出图 {item.today_images}</div>
                </TableCell>
                <TableCell className="text-right tabular-nums text-sm">{limitText(item.used_requests, item.total_limit)}</TableCell>
                <TableCell className="text-right tabular-nums text-sm">{limitText(item.used_images, item.image_limit)}</TableCell>
                <TableCell className="text-right tabular-nums text-sm">
                  {item.rpm_limit || "∞"} / {item.concurrency_limit || "∞"}
                  {item.running > 0 ? <div className="text-xs text-muted-foreground">进行中 {item.running}</div> : null}
                </TableCell>
                <TableCell className="max-w-[160px] truncate pl-8 text-xs text-muted-foreground" title={(item.models || []).join(", ")}>
                  {(item.models || []).length ? item.models.join(", ") : "全部"}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">{formatTime(item.last_used_at)}</TableCell>
                <TableCell className="text-right">
                  <div className="-mr-2.5 flex justify-end">
                    <Button variant="ghost" size="icon" onClick={() => openEdit(item)} title="编辑">
                      <Pencil className="size-4" />
                    </Button>
                    <Button variant="ghost" size="icon" onClick={() => void reset(item)} title="重置用量">
                      <RotateCcw className="size-4" />
                    </Button>
                    <Button variant="ghost" size="icon" onClick={() => void remove(item)} title="删除">
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </section>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑 API Key" : "新建 API Key"}</DialogTitle>
            <DialogDescription>0 表示不限制。Key 只保存哈希，完整值仅在创建时显示一次。</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3 text-sm">
            <label className="flex flex-col gap-1">
              <span className="text-xs text-muted-foreground">名称</span>
              <Input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder="例如：客户端 A" />
            </label>
            {!editing ? (
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">自定义 Key（留空自动生成，长度不限）</span>
                <Input value={form.key} onChange={(event) => setForm({ ...form, key: event.target.value })} placeholder="sk-..." />
              </label>
            ) : null}
            <label className="flex items-center gap-2">
              <Checkbox checked={form.enabled} onCheckedChange={(value) => setForm({ ...form, enabled: Boolean(value) })} />
              启用
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-xs text-muted-foreground">过期时间（可选）</span>
              <Input type="datetime-local" value={form.expires_at} onChange={(event) => setForm({ ...form, expires_at: event.target.value })} />
            </label>
            <div className="grid grid-cols-2 gap-3">
              {limitFields.map(([field, label]) => (
                <label key={field} className="flex flex-col gap-1">
                  <span className="text-xs text-muted-foreground">{label}</span>
                  <Input type="number" min={0} value={String(form[field])} onChange={(event) => setForm({ ...form, [field]: event.target.value })} />
                </label>
              ))}
            </div>
            <label className="flex flex-col gap-1">
              <span className="text-xs text-muted-foreground">模型白名单（逗号分隔，留空表示全部）</span>
              <Input value={form.models} onChange={(event) => setForm({ ...form, models: event.target.value })} placeholder="gpt-image-2.6, auto" />
            </label>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
            <Button onClick={() => void save()} disabled={saving}>
              {saving ? <LoaderCircle className="size-4 animate-spin" /> : null}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(createdKey)} onOpenChange={(open) => !open && setCreatedKey("")}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Key 已创建</DialogTitle>
            <DialogDescription>请立即复制保存，关闭后无法再次查看完整 Key。</DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 break-all rounded-lg bg-muted px-3 py-2 font-mono text-sm">{createdKey}</code>
            <Button variant="outline" size="icon" onClick={() => copy(createdKey)}>
              <Copy className="size-4" />
            </Button>
          </div>
          <DialogFooter>
            <Button onClick={() => setCreatedKey("")}>我已保存</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

export default function KeysPage() {
  return (
    <AdminGuard path="/keys">
      <KeysContent />
    </AdminGuard>
  );
}
