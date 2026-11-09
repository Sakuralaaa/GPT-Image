import type { ReactNode } from "react";
import { LoaderCircle } from "lucide-react";

import { useAuthGuard } from "@/lib/use-auth-guard";
import { cn } from "@/lib/utils";

export function AdminGuard({ path, children }: { path: string; children: ReactNode }) {
  const { isCheckingAuth, session } = useAuthGuard(undefined, path);
  if (isCheckingAuth || !session) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center">
        <LoaderCircle className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }
  return <>{children}</>;
}

/** Row of editorial stats separated by vertical hairlines. */
export function StatRow({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <section className={cn("grid grid-cols-2 border-y border-rule-strong md:grid-cols-4 [&>*]:border-border [&>*:nth-child(n+3)]:border-t md:[&>*:nth-child(n+3)]:border-t-0 [&>*:nth-child(even)]:border-l md:[&>*:not(:first-child)]:border-l", className)}>
      {children}
    </section>
  );
}

/**
 * One stat: small label, large serif tabular number, optional unit and hint.
 * `tone` is accepted for compatibility but no longer colours the box.
 */
export function StatCard({
  label,
  value,
  unit,
  hint,
}: {
  label: string;
  value: ReactNode;
  unit?: ReactNode;
  hint?: ReactNode;
  tone?: "default" | "success" | "warning" | "danger";
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5 px-6 py-6 first:pl-0 max-md:[&:nth-child(3)]:pl-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="flex items-baseline gap-1.5">
        <span className="num truncate font-serif text-[2.5rem] leading-none font-normal text-foreground">{value}</span>
        {unit ? <span className="text-xs text-muted-foreground">{unit}</span> : null}
      </div>
      {hint ? <div className="num text-xs text-muted-foreground">{hint}</div> : null}
    </div>
  );
}
