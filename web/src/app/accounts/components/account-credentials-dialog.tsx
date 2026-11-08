import { useState } from "react";
import { Download, FileUp, LoaderCircle } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import {
  downloadTextFile,
  exportAccountCredentials,
  importAccountCredentials,
  type ImportCredentialsResult,
} from "@/lib/admin-api";

type Props = {
  selectedIds: string[];
  onImported: () => void;
};

export function AccountCredentialsDialog({ selectedIds, onImported }: Props) {
  const [exportOpen, setExportOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [content, setContent] = useState("");
  const [result, setResult] = useState<ImportCredentialsResult | null>(null);

  const doExport = async (format: "json" | "csv") => {
    setBusy(true);
    try {
      const data = await exportAccountCredentials(format, selectedIds);
      const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-");
      if (format === "csv") {
        downloadTextFile(`gpt-image-accounts-${stamp}.csv`, `\ufeff${data.content || ""}`, "text/csv");
      } else {
        downloadTextFile(`gpt-image-accounts-${stamp}.json`, JSON.stringify(data.items || [], null, 2), "application/json");
      }
      toast.success(`已导出 ${data.count} 个账号`);
      setExportOpen(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "导出失败");
    } finally {
      setBusy(false);
    }
  };

  const doImport = async () => {
    setBusy(true);
    try {
      const data = await importAccountCredentials(content);
      setResult(data);
      toast.success(`新增 ${data.added}，更新 ${data.updated}，跳过 ${data.skipped}`);
      onImported();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "导入失败");
    } finally {
      setBusy(false);
    }
  };

  const readFile = (file: File | undefined) => {
    if (!file) return;
    void file.text().then(setContent);
  };

  return (
    <>
      <Button variant="outline" className="h-10 rounded-lg" onClick={() => setExportOpen(true)}>
        <Download className="size-4" />
        导出凭证{selectedIds.length ? `（${selectedIds.length}）` : ""}
      </Button>
      <Button variant="outline" className="h-10 rounded-lg" onClick={() => { setResult(null); setImportOpen(true); }}>
        <FileUp className="size-4" />
        导入凭证
      </Button>

      <Dialog open={exportOpen} onOpenChange={setExportOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>导出账号凭证</DialogTitle>
            <DialogDescription>
              导出文件包含明文密码、邮箱凭证与 Token，请妥善保管。{selectedIds.length ? `将导出已选的 ${selectedIds.length} 个账号。` : "未选择账号，将导出全部账号。"}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter className="gap-2">
            <Button variant="outline" onClick={() => setExportOpen(false)}>取消</Button>
            <Button variant="outline" disabled={busy} onClick={() => void doExport("csv")}>确认导出 CSV</Button>
            <Button disabled={busy} onClick={() => void doExport("json")}>
              {busy ? <LoaderCircle className="size-4 animate-spin" /> : null}
              确认导出 JSON
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={importOpen} onOpenChange={setImportOpen}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>导入账号凭证</DialogTitle>
            <DialogDescription>
              支持本系统导出的 JSON / CSV（需 email 列）。按邮箱合并：已存在的账号更新凭证，新邮箱需带 access_token 才能入库。
            </DialogDescription>
          </DialogHeader>
          <input type="file" accept=".json,.csv,text/csv,application/json" onChange={(event) => readFile(event.target.files?.[0])} className="text-sm" />
          <Textarea className="min-h-48 font-mono text-xs" placeholder="或直接粘贴 JSON / CSV 内容" value={content} onChange={(event) => setContent(event.target.value)} />
          {result ? (
            <div className="rounded-lg border border-border/70 p-3 text-xs">
              <div>新增 {result.added} · 更新 {result.updated} · 跳过 {result.skipped}</div>
              {result.errors.length ? (
                <ul className="mt-2 max-h-32 list-disc overflow-auto pl-4 text-muted-foreground">
                  {result.errors.map((item, index) => (
                    <li key={index}>{item.row} {item.email || ""}：{item.error}</li>
                  ))}
                </ul>
              ) : null}
            </div>
          ) : null}
          <DialogFooter>
            <Button variant="outline" onClick={() => setImportOpen(false)}>关闭</Button>
            <Button disabled={busy || !content.trim()} onClick={() => void doImport()}>
              {busy ? <LoaderCircle className="size-4 animate-spin" /> : null}
              导入
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
