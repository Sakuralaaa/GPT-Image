import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { AdminGuard, StatCard, StatRow } from "@/components/admin-page";
import { formatTime, limitText } from "@/lib/format";
import { cn } from "@/lib/utils";
import { EmptyState, PageHeader, Section, StatusDot } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { fetchDashboard, RECOVERY_STATUS_LABELS, type DashboardData, type RecoveryStatus } from "@/lib/admin-api";

const STATUS_TONES: Record<string, "ok" | "warn" | "bad" | "neutral"> = {
  正常: "ok",
  限流: "warn",
  过期待刷新: "warn",
  刷新中: "neutral",
  异常: "bad",
  禁用: "neutral",
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

  const keyList = data?.key_list || [];
  const recoveryCounts = Object.entries(data?.recovery.counts || {});

  return (
    <>
      <PageHeader
        eyebrow="Overview"
        title="仪表盘"
        dek="号池、API Key 与找回任务的当日概况，每 30 秒自动刷新。"
        actions={
          <Button variant="outline" onClick={() => void load()} disabled={loading}>
            <RefreshCw className={loading ? "size-4 animate-spin" : "size-4"} />
            刷新
          </Button>
        }
      />
      <StatRow>
        <StatCard label="账号总数" value={data?.accounts.total ?? "—"} unit="个" hint={`启用 ${data?.accounts.enabled ?? 0}`} />
        <StatCard label="剩余图片额度" value={data?.accounts.quota ?? "—"} unit="张" />
        <StatCard label="今日调用" value={data?.keys.today_requests ?? "—"} unit="次" hint={`今日出图 ${data?.keys.today_images ?? 0}`} />
        <StatCard label="API Key" value={data?.keys.keys ?? "—"} unit="个" hint={`可用 ${data?.keys.enabled_keys ?? 0}`} />
      </StatRow>
      <Section title="号池状态" kicker="Pool" action={<Link to="/accounts" className="text-oxblood hover:underline">管理账号 →</Link>}>
        {statuses.length === 0 ? (
          <EmptyState title="暂无账号">在账号页导入或添加 ChatGPT 账号。</EmptyState>
        ) : (
          <div className="-mt-4 grid grid-cols-2 border-b border-border md:grid-cols-4">
            {[
              ...statuses.map(([status, count]) => ({
                key: status,
                label: <StatusDot tone={STATUS_TONES[status] || "neutral"}>{status}</StatusDot>,
                value: <span className="num font-serif text-2xl leading-none text-foreground">{count}</span>,
              })),
              {
                key: "__type",
                label: <span className="text-[13px] text-muted-foreground">类型</span>,
                value: (
                  <span className="num text-sm leading-6 text-foreground">
                    {Object.entries(data?.accounts.by_type || {}).map(([type, count]) => `${type} ${count}`).join(" / ") || "—"}
                  </span>
                ),
              },
            ].map((item, index) => (
              <div
                key={item.key}
                className={cn(
                  "flex min-h-[84px] min-w-0 flex-col justify-center gap-2 border-border px-6 py-4",
                  index % 2 === 0 ? "max-md:pl-0" : "max-md:border-l",
                  index % 4 === 0 ? "md:pl-0" : "md:border-l",
                  index >= 2 && "max-md:border-t",
                  index >= 4 && "md:border-t",
                )}
              >
                {item.label}
                {item.value}
              </div>
            ))}
          </div>
        )}
      </Section>
      <Section title="各 Key 用量" kicker="Usage" action={<Link to="/keys" className="text-oxblood hover:underline">管理 Key →</Link>}>
        {keyList.length === 0 ? (
          <EmptyState title="还没有 API Key">在 API Key 页创建一个独立 Key，设置额度与模型白名单。</EmptyState>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-0">名称</TableHead>
                  <TableHead className="text-right">今日请求</TableHead>
                  <TableHead className="text-right">今日出图</TableHead>
                  <TableHead className="text-right">累计请求</TableHead>
                  <TableHead className="text-right">累计出图</TableHead>
                  <TableHead className="text-right">最近使用</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {keyList.map((key) => (
                  <TableRow key={key.id}>
                    <TableCell className="pl-0">
                      <div className="font-medium">{key.name}</div>
                      <div className="font-mono text-xs text-muted-foreground">{key.key_prefix}</div>
                    </TableCell>
                    <TableCell className="text-right">{limitText(key.today_requests, key.daily_limit)}</TableCell>
                    <TableCell className="text-right">{key.today_images}</TableCell>
                    <TableCell className="text-right">{limitText(key.used_requests, key.total_limit)}</TableCell>
                    <TableCell className="text-right">{limitText(key.used_images, key.image_limit)}</TableCell>
                    <TableCell className="text-right text-xs text-muted-foreground">{formatTime(key.last_used_at)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Section>
      <Section title="找回任务" kicker="Recovery" action={<Link to="/recovery" className="text-oxblood hover:underline">查看任务 →</Link>}>
        <div className="flex flex-wrap items-center gap-x-8 gap-y-2 text-sm">
          <StatusDot tone={data?.recovery.auto_enabled ? "ok" : "neutral"}>自动自愈{data?.recovery.auto_enabled ? "已开启" : "未开启"}</StatusDot>
          {recoveryCounts.length === 0 ? <span className="text-muted-foreground">当前没有任务</span> : null}
          {recoveryCounts.map(([status, count]) => (
            <span key={status} className="text-muted-foreground">
              {RECOVERY_STATUS_LABELS[status as RecoveryStatus] || status} <span className="num font-serif text-base text-foreground">{count}</span>
            </span>
          ))}
        </div>
      </Section>
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
