import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

// Editorial badges: small text with a status dot, hairline outline only for neutral tags.
const badgeVariants = cva(
  "inline-flex items-center gap-1.5 whitespace-nowrap rounded-[6px] text-xs font-medium leading-5 transition-colors",
  {
    variants: {
      variant: {
        default: "text-oxblood before:size-1.5 before:rounded-full before:bg-oxblood before:content-['']",
        secondary: "text-muted-foreground before:size-1.5 before:rounded-full before:bg-muted-foreground/60 before:content-['']",
        outline: "border border-border px-1.5 text-foreground",
        success: "text-foreground before:size-1.5 before:rounded-full before:bg-emerald-600 before:content-['']",
        warning: "text-foreground before:size-1.5 before:rounded-full before:bg-amber-600 before:content-['']",
        danger: "text-rose-700 before:size-1.5 before:rounded-full before:bg-rose-600 before:content-['']",
        info: "text-foreground before:size-1.5 before:rounded-full before:bg-oxblood before:content-['']",
        violet: "text-foreground before:size-1.5 before:rounded-full before:bg-oxblood before:content-['']",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

function Badge({
  className,
  variant,
  ...props
}: React.ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
  return (
    <span className={cn(badgeVariants({ variant }), className)} {...props} />
  );
}

export { Badge, badgeVariants };
