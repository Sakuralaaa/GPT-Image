import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

type PageHeaderProps = {
  eyebrow: string;
  title: string;
  dek?: ReactNode;
  actions?: ReactNode;
  className?: string;
};

/**
 * Editorial page header: kicker, serif title, one-line dek, actions aligned to
 * the title, then a hairline rule. Spacing below is owned by the page flow.
 */
export function PageHeader({ eyebrow, title, dek, actions, className }: PageHeaderProps) {
  return (
    <header className={cn("flex flex-col gap-4 md:flex-row md:items-end md:justify-between md:gap-8", className)}>
      <div className="flex min-w-0 flex-col gap-2">
        <div className="kicker">{eyebrow}</div>
        <h1 className="font-serif text-[2.25rem] leading-[1.1] font-medium tracking-tight text-foreground sm:text-[2.75rem]">{title}</h1>
        {dek ? <p className="max-w-2xl text-[15px] leading-6 text-muted-foreground">{dek}</p> : null}
      </div>
      {actions ? <div className={cn("flex shrink-0 flex-wrap items-center gap-2", dek ? "md:-mb-1.5" : "md:mb-1.5")}>{actions}</div> : null}
    </header>
  );
}

/** A titled section with a hairline under its heading; no box around it. */
export function Section({
  title,
  kicker,
  action,
  children,
  className,
}: {
  title: ReactNode;
  kicker?: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("flex min-w-0 flex-col", className)}>
      <div className="flex items-end justify-between gap-4 border-b border-rule-strong pb-2">
        <div className="flex min-w-0 flex-col gap-1">
          {kicker ? <span className="kicker">{kicker}</span> : null}
          <h2 className="font-serif text-xl leading-tight font-medium">{title}</h2>
        </div>
        {action ? <div className="flex shrink-0 items-center gap-3 text-sm">{action}</div> : null}
      </div>
      <div className="pt-4">{children}</div>
    </section>
  );
}

/** Left-aligned, modest empty state that sits on the grid. */
export function EmptyState({ title, children, className }: { title: ReactNode; children?: ReactNode; className?: string }) {
  return (
    <div className={cn("py-2", className)}>
      <p className="font-serif text-lg leading-snug text-foreground">{title}</p>
      {children ? <p className="mt-1 max-w-md text-sm leading-6 text-muted-foreground">{children}</p> : null}
    </div>
  );
}

/** Status as a small dot + text, replacing coloured pills. */
export function StatusDot({ tone = "neutral", children, className }: { tone?: "ok" | "warn" | "bad" | "accent" | "neutral"; children: ReactNode; className?: string }) {
  const dot = {
    ok: "bg-emerald-600",
    warn: "bg-amber-600",
    bad: "bg-rose-600",
    accent: "bg-oxblood",
    neutral: "bg-muted-foreground/60",
  }[tone];
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-[13px] text-foreground", className)}>
      <span className={cn("size-1.5 shrink-0 rounded-full", dot)} />
      {children}
    </span>
  );
}
