"use client";

import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";

import { cn } from "@/lib/utils";

type SettingsCardTone = "blue" | "amber" | "slate" | "violet";

type SettingsCardProps = {
  action?: ReactNode;
  children: ReactNode;
  className?: string;
  contentClassName?: string;
  description: string;
  icon?: LucideIcon;
  meta?: ReactNode;
  title: string;
  tone?: SettingsCardTone;
};

export const settingsInputClassName = "bg-card";
export const settingsDialogInputClassName = "bg-card";
export const settingsInlineCodeClassName =
  "min-w-0 break-all rounded-sm bg-muted px-2 py-1 font-mono text-xs leading-5 text-muted-foreground";
export const settingsListItemClassName =
  "border-b border-border py-4";
export const settingsPanelClassName =
  "border-l-2 border-border pl-4";
export const settingsToggleClassName =
  "flex min-h-12 items-center gap-3 border-b border-border py-3 text-sm text-foreground";
export const settingsInlineToggleClassName =
  "flex min-h-9 items-center gap-3 py-2 text-sm text-foreground";

/**
 * Settings section in a magazine-column layout: title + dek on the left,
 * the form on the right, a strong hairline above. No card, no icon tile.
 */
export function SettingsCard({
  action,
  children,
  className,
  contentClassName,
  description,
  meta,
  title,
}: SettingsCardProps) {
  return (
    <section className={cn("grid gap-6 border-t border-rule-strong pt-6 lg:grid-cols-[240px_minmax(0,1fr)] lg:gap-12", className)}>
      <div className="flex min-w-0 flex-col gap-2">
        <h2 className="font-serif text-xl leading-tight font-medium">{title}</h2>
        {description ? <p className="text-[13px] leading-6 text-muted-foreground">{description}</p> : null}
        {meta || action ? (
          <div className="mt-2 flex flex-wrap items-center gap-2">
            {meta}
            {action}
          </div>
        ) : null}
      </div>
      <div className={cn("min-w-0", contentClassName, "p-0 sm:p-0")}>{children}</div>
    </section>
  );
}

export function SettingsNotice({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("border-l-2 border-oxblood/60 py-1 pl-4 text-[13px] leading-6 text-muted-foreground", className)}>
      {children}
    </div>
  );
}

export function SettingsEmptyState({
  description,
  title,
}: {
  description: string;
  icon?: LucideIcon;
  title: string;
}) {
  return (
    <div className="py-1">
      <p className="font-serif text-lg text-foreground">{title}</p>
      <p className="mt-1 max-w-md text-sm leading-6 text-muted-foreground">{description}</p>
    </div>
  );
}
