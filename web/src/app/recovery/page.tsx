import { useCallback, useEffect, useMemo, useState } from "react";
import { LoaderCircle, Play, Plus, RotateCcw, Send, Square, Trash2, Zap } from "lucide-react";
import { toast } from "sonner";

import { AdminGuard } from "@/components/admin-page";
import { formatTime } from "@/lib/format";
import { EmptyState, PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import {
  MAIL_CHANNEL_LABELS,
  RECOVERY_STATUS_LABELS,
  cancelRecoveryJob,
  createRecoveryJob,
  deleteRecoveryJob,
  fetchRecoveryJobs,
  retryRecoveryJob,
  runAutoHealNow,
  submitRecoveryCodes,
  type RecoveryItem,
  type RecoveryJob,
} from "@/lib/admin-api";
import { cn } from "@/lib/utils";

const statusVariant: Record<string, "success" | "warning" | "danger" | "secondary" | "info" | "outline"> = {
  queued: "secondary",
  running: "info",
  awaiting_code: "warning",
  succeeded: "success",
  failed: "danger",
  cancelled: "outline",
};

const methodLabels: Record<string, string> = {
  refresh: "刷新 Token",
  password: "密码登录",
  email_otp: "邮箱验证码",
};

function StatusBadge({ status }: { status: string }) {
  return <Badge variant={statusVariant[status] || "secondary"}>{RECOVERY_STATUS_LABELS[status as keyof typeof RECOVERY_STATUS_LABELS] || status}</Badge>;
}

function RecoveryContent() {
  const [jobs, setJobs] = useState<RecoveryJob[]>([]);
  const [activeId, setActiveId] = useState("");
  const [loading, setLoading] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [emails, setEmails] = useState("");
  const [concurrency, setConcurrency] = useState("3");
  const [name, setName] = useState("");
  const [codes, setCodes] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [statusFilter, setStatusFilter] = useState("all");

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const data = await fetchRecoveryJobs();
      setJobs(data.items);
      setActiveId((current) => current || data.items[0]?.id || "");
    } catch (error) {
      if (!silent) toast.error(error instanceof Error ? error.message : "加载失败");
    } finally {
      if (!silent) setLoading(false);
    }
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(timer);
  }, [load]);

  const active = jobs.find((job) => job.id === activeId) || null;
  const hasLive = jobs.some((job) => job.status === "running");

  useEffect(() => {
    const timer = window.setInterval(() => void load(true), hasLive ? 2000 : 8000);
    return () => window.clearInterval(timer);
  }, [hasLive, load]);

  const visibleItems = useMemo(
    () => (active?.items || []).filter((item) => statusFilter === "all" || item.status === statusFilter),
    [active, statusFilter],
  );
  const awaiting = (active?.items || []).filter((item) => item.status === "awaiting_code");

  const run = async (fn: () => Promise<unknown>, success?: string) => {
    setBusy(true);
    try {
      await fn();
      if (success) toast.success(success);
      await load(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "操作失败");
    } finally {
      setBusy(false);
    }
  };

  const create = () =>
    run(async () => {
      const data = await createRecoveryJob({ name: name.trim(), emails_text: emails, concurrency: Number(concurrency) || 3 });
      setActiveId(data.item.id);
      setCreateOpen(false);
      setEmails("");
      setName("");
    }, "任务已创建，后台执行中");

  const submitCodes = (onlyItem?: RecoveryItem) =>
    run(async () => {
      if (!active) return;
      const payload: Record<string, string> = {};
      for (const item of onlyItem ? [onlyItem] : awaiting) {
        const code = (codes[item.id] || "").trim();
        if (code) payload[item.id] = code;
      }
      if (Object.keys(payload).length === 0) throw new Error("请先填写验证码");
      const data = await submitRecoveryCodes(active.id, payload);
      const errorCount = Object.keys(data.errors || {}).length;
      toast.success(`已提交 ${data.accepted} 个验证码${errorCount ? `，${errorCount} 个失败` : ""}`);
      setCodes((prev) => {
        const next = { ...prev };
        Object.keys(payload).forEach((key) => delete next[key]);
        return next;
      });
    });

  return (
    <>
      <PageHeader
        eyebrow="Recovery"
        title="找回任务"
        dek="刷新 Token、已存密码登录、邮箱验证码登录依次尝试；收不到验证码的条目进入“待填码”。"
        actions={
          <>
            <Button variant="outline" onClick={() => void run(async () => {
              const data = await runAutoHealNow();
              toast.success(`自愈扫描完成，加入 ${data.queued} 个账号`);
            })} disabled={busy}>
              <Zap className="size-4" />
              立即自愈扫描
            </Button>
            <Button onClick={() => setCreateOpen(true)}>
              <Plus className="size-4" />
              新建任务
            </Button>
          </>
        }
      />
      {loading && jobs.length === 0 ? <LoaderCircle className="size-5 animate-spin text-muted-foreground" /> : null}
      {!loading && jobs.length === 0 ? (
        <EmptyState title="暂无找回任务">在账号页多选后“加入找回”，或点击右上角“新建任务”粘贴邮箱。开启自动自愈后，异常账号会自动入队。</EmptyState>
      ) : null}
      {jobs.length > 0 ? (
      <div className="grid gap-8 lg:grid-cols-[280px_minmax(0,1fr)]">
        <aside className="flex flex-col border-t border-rule-strong">
          {jobs.map((job) => (
            <button
              key={job.id}
              type="button"
              onClick={() => setActiveId(job.id)}
              className={cn(
                "border-b border-border py-3 text-left transition-colors",
                job.id === activeId ? "" : "opacity-80 hover:opacity-100",
              )}
            >
              <div className="flex items-center justify-between gap-2">
                <span className={cn("truncate text-sm font-medium", job.id === activeId && "text-oxblood")}>{job.name}</span>
                {job.status === "running" ? <LoaderCircle className="size-3.5 animate-spin text-primary" /> : null}
              </div>
              <div className="mt-1 text-xs text-muted-foreground">{formatTime(job.created_at)} · {job.source === "auto" ? "自动" : "手动"}</div>
              <div className="mt-1 flex flex-wrap gap-2 text-xs">
                <span className="text-emerald-600">成功 {job.counts.succeeded || 0}</span>
                <span className="text-amber-600">待填码 {job.counts.awaiting_code || 0}</span>
                <span className="text-rose-600">失败 {job.counts.failed || 0}</span>
                <span className="text-muted-foreground">/ {job.total}</span>
              </div>
            </button>
          ))}
        </aside>
        <section className="min-w-0 border-t border-rule-strong pt-3">
          {!active ? (
            <EmptyState title="未选择任务">从左侧列表选择一个任务，查看每个邮箱的进度与验证码。</EmptyState>
          ) : (
            <>
              <div className="mb-4 flex flex-wrap items-center justify-between gap-4">
                <div>
                  <h2 className="font-serif text-xl font-medium">{active.name}</h2>
                  <div className="text-xs text-muted-foreground">并发 {active.concurrency} · 更新于 {formatTime(active.updated_at)}</div>
                </div>
                <div className="flex flex-wrap gap-2">
                  <Select value={statusFilter} onValueChange={setStatusFilter}>
                    <SelectTrigger className="w-36">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">全部状态</SelectItem>
                      {Object.entries(RECOVERY_STATUS_LABELS).map(([value, label]) => (
                        <SelectItem key={value} value={value}>{label} {active.counts[value] || 0}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button variant="outline" disabled={busy} onClick={() => void run(() => retryRecoveryJob(active.id), "已重新排队失败条目")}>
                    <RotateCcw className="size-4" />
                    重试失败
                  </Button>
                  <Button variant="outline" disabled={busy} onClick={() => void run(() => cancelRecoveryJob(active.id), "已取消")}>
                    <Square className="size-4" />
                    取消
                  </Button>
                  <Button variant="outline" size="icon" title="删除任务" disabled={busy} onClick={() => {
                    if (!window.confirm("删除该任务记录？")) return;
                    void run(async () => {
                      await deleteRecoveryJob(active.id);
                      setActiveId("");
                    });
                  }}>
                    <Trash2 className="size-4" />
                  </Button>
                </div>
              </div>
              {awaiting.length > 0 ? (
                <div className="flex flex-wrap items-center justify-between gap-4 border-t border-border py-3 text-sm text-foreground">
                  <span className="flex items-center gap-2"><span className="size-1.5 shrink-0 rounded-full bg-amber-600" />{awaiting.length} 个账号等待验证码：在下方逐个填写后批量提交（登录会话约 20 分钟内有效）。</span>
                  <Button disabled={busy} onClick={() => void submitCodes()}>
                    <Send className="size-4" />
                    批量提交验证码
                  </Button>
                </div>
              ) : null}
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>邮箱</TableHead>
                      <TableHead>状态</TableHead>
                      <TableHead>收码渠道</TableHead>
                      <TableHead>阶段 / 原因</TableHead>
                      <TableHead className="text-right">次数</TableHead>
                      <TableHead className="pl-8">更新时间</TableHead>
                      <TableHead className="text-right">操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {visibleItems.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell className="font-mono text-xs">{item.email}</TableCell>
                        <TableCell>
                          <StatusBadge status={item.status} />
                          {item.method ? <div className="mt-1 text-xs text-muted-foreground">{methodLabels[item.method] || item.method}</div> : null}
                        </TableCell>
                        <TableCell className="text-xs">{item.channel ? MAIL_CHANNEL_LABELS[item.channel] || item.channel : "—"}</TableCell>
                        <TableCell className="max-w-[380px] text-xs">
                          {item.stage ? <div className="font-medium">{item.stage}</div> : null}
                          {item.error ? <div className="whitespace-pre-wrap break-words text-muted-foreground">{item.error}</div> : null}
                          {item.token_preview ? <div className="font-mono text-emerald-600">{item.token_preview}</div> : null}
                        </TableCell>
                        <TableCell className="text-right tabular-nums text-xs">{item.attempts}</TableCell>
                        <TableCell className="whitespace-nowrap pl-8 text-xs text-muted-foreground">{formatTime(item.updated_at)}</TableCell>
                        <TableCell className="text-right">
                          {item.status === "awaiting_code" ? (
                            <div className="flex items-center justify-end gap-2">
                              <Input
                                className="w-28 font-mono"
                                placeholder="验证码"
                                value={codes[item.id] || ""}
                                onChange={(event) => setCodes((prev) => ({ ...prev, [item.id]: event.target.value }))}
                                onKeyDown={(event) => {
                                  if (event.key === "Enter") void submitCodes(item);
                                }}
                              />
                              <Button size="icon" disabled={busy} onClick={() => void submitCodes(item)} title="提交">
                                <Play className="size-4" />
                              </Button>
                            </div>
                          ) : item.status === "failed" || item.status === "cancelled" ? (
                            <Button variant="ghost" size="sm" className="-mr-3" disabled={busy} onClick={() => void run(() => retryRecoveryJob(active.id, [item.id]), "已重新排队")}>
                              <RotateCcw className="size-4" />
                              重试
                            </Button>
                          ) : item.status === "queued" || item.status === "running" ? (
                            <Button variant="ghost" size="sm" className="-mr-3" disabled={busy} onClick={() => void run(() => cancelRecoveryJob(active.id, [item.id]))}>
                              取消
                            </Button>
                          ) : null}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </>
          )}
        </section>
      </div>
      ) : null}

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>新建找回任务</DialogTitle>
            <DialogDescription>
              每个邮箱依次尝试：刷新 Token → 已存密码登录 → 邮箱验证码登录（按域名路由的渠道自动收码，收不到则进入“待填码”）。
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3 text-sm">
            <Input placeholder="任务名称（可选）" value={name} onChange={(event) => setName(event.target.value)} />
            <Textarea
              className="min-h-40 font-mono text-xs"
              placeholder={"每行一个邮箱，也可用逗号/空格分隔"}
              value={emails}
              onChange={(event) => setEmails(event.target.value)}
            />
            <label className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground">并发</span>
              <Input type="number" min={1} max={20} className="w-24" value={concurrency} onChange={(event) => setConcurrency(event.target.value)} />
            </label>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
            <Button onClick={() => void create()} disabled={busy || !emails.trim()}>
              {busy ? <LoaderCircle className="size-4 animate-spin" /> : null}
              开始
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

export default function RecoveryPage() {
  return (
    <AdminGuard path="/recovery">
      <RecoveryContent />
    </AdminGuard>
  );
}
