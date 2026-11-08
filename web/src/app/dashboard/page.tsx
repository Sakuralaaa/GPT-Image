import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { AdminGuard, StatCard } from "@/components/admin-page";
import { formatTime, limitText } from "@/lib/format";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { fetchDashboard, RECOVERY_STATUS_LABELS, type DashboardData, type RecoveryStatus } from "@/lib/admin-api";

const STATUS_TONES: Record<string, "success" | "warning" | "danger" | "default"> = {
  正常: "success",
  限流: "warning",
  过期待刷新: "warning",
  刷新中: "default",
  异常: "danger",
  禁用: "default",
};

function DashboardContent() {
  const [data, setData] = useState<DashboardData | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setData(await fetchDashboard());
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const first = window.setTimeout(() => void load(), 0);
    const timer = window.setInterval(() => void load(), 30000);
    return () => {
      window.clearTimeout(first);
      window.clearInterval(timer);
    };
  }, [load]);

  const statuses = Object.entries(data?.accounts.by_status || {}).sort((a, b) => b[1] - a[1]);

  return (
    <>
      <PageHeader
        eyebrow="Overview"
        title="仪表盘"
        actions={
          <Button variant="outline" onClick={() => void load()} disabled={loading}>
            <RefreshCw className={loading ? "size-4 animate-spin" : "size-4"} />
            刷新
          </Button>
        }
      />
      <section className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatCard label="账号总数" value={data?.accounts.total ?? "—"} hint={`启用 ${data?.accounts.enabled ?? 0}`} />
        <StatCard label="剩余图片额度" value={data?.accounts.quota ?? "—"} />
        <StatCard label="今日调用" value={data?.keys.today_requests ?? "—"} hint={`今日出图 ${data?.keys.today_images ?? 0}`} tone="success" />
        <StatCard label="API Key" value={data?.keys.keys ?? "—"} hint={`可用 ${data?.keys.enabled_keys ?? 0}`} />
      </section>
      <section className="grid gap-4 lg:grid-cols-3">
        <div className="rounded-xl border border-border/70 bg-card p-4 shadow-sm">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="font-semibold">号池状态</h2>
            <Link to="/accounts" className="text-xs text-primary hover:underline">管理账号</Link>
          </div>
          <div className="flex flex-col gap-2">
            {statuses.length === 0 ? <span className="text-sm text-muted-foreground">暂无账号</span> : null}
            {statuses.map(([status, count]) => (
              <div key={status} className="flex items-center justify-between text-sm">
                <Badge variant={STATUS_TONES[status] === "success" ? "success" : STATUS_TONES[status] === "warning" ? "warning" : STATUS_TONES[status] === "danger" ? "danger" : "secondary"}>{status}</Badge>
                <span className="tabular-nums">{count}</span>
              </div>
            ))}
          </div>
          <div className="mt-4 border-t border-border/60 pt-3 text-xs text-muted-foreground">
            类型：{Object.entries(data?.accounts.by_type || {}).map(([type, count]) => `${type} ${count}`).join(" · ") || "—"}
          </div>
        </div>
        <div className="rounded-xl border border-border/70 bg-card p-4 shadow-sm lg:col-span-2">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="font-semibold">各 Key 用量</h2>
            <Link to="/keys" className="text-xs text-primary hover:underline">管理 Key</Link>
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>名称</TableHead>
                <TableHead>今日请求</TableHead>
                <TableHead>今日出图</TableHead>
                <TableHead>累计请求</TableHead>
                <TableHead>累计出图</TableHead>
                <TableHead>最近使用</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(data?.key_list || []).map((key) => (
                <TableRow key={key.id}>
                  <TableCell>
                    <div className="font-medium">{key.name}</div>
                    <div className="font-mono text-xs text-muted-foreground">{key.key_prefix}</div>
                  </TableCell>
                  <TableCell className="tabular-nums">{limitText(key.today_requests, key.daily_limit)}</TableCell>
                  <TableCell className="tabular-nums">{key.today_images}</TableCell>
                  <TableCell className="tabular-nums">{limitText(key.used_requests, key.total_limit)}</TableCell>
                  <TableCell className="tabular-nums">{limitText(key.used_images, key.image_limit)}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{formatTime(key.last_used_at)}</TableCell>
                </TableRow>
              ))}
              {(data?.key_list || []).length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-sm text-muted-foreground">还没有 API Key</TableCell>
                </TableRow>
              ) : null}
            </TableBody>
          </Table>
        </div>
      </section>
      <section className="rounded-xl border border-border/70 bg-card p-4 shadow-sm">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="font-semibold">找回任务</h2>
          <Link to="/recovery" className="text-xs text-primary hover:underline">查看任务</Link>
        </div>
        <div className="flex flex-wrap gap-2 text-sm">
          <Badge variant={data?.recovery.auto_enabled ? "success" : "secondary"}>自动自愈{data?.recovery.auto_enabled ? "已开启" : "未开启"}</Badge>
          {Object.entries(data?.recovery.counts || {}).map(([status, count]) => (
            <Badge key={status} variant="outline">{RECOVERY_STATUS_LABELS[status as RecoveryStatus] || status} {count}</Badge>
          ))}
        </div>
      </section>
    </>
  );
}

export default function DashboardPage() {
  return (
    <AdminGuard path="/dashboard">
      <DashboardContent />
    </AdminGuard>
  );
}
