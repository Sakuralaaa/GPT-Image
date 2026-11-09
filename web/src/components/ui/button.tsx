import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-sm text-sm font-medium transition-colors outline-none disabled:pointer-events-none disabled:opacity-45 [&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-4 [&_svg]:shrink-0 focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40",
  {
    variants: {
      variant: {
        default:
          "border border-primary bg-primary text-primary-foreground hover:bg-primary/90",
        destructive:
          "border border-destructive bg-transparent text-destructive hover:bg-destructive hover:text-background",
        outline:
          "border border-input bg-transparent text-foreground hover:border-ink-4/70 hover:bg-accent",
        secondary:
          "border border-input bg-transparent text-foreground hover:border-ink-4/70 hover:bg-accent",
        ghost:
          "border border-transparent text-muted-foreground hover:bg-accent hover:text-foreground",
        link: "h-auto! px-0! text-oxblood underline decoration-oxblood/40 underline-offset-4 hover:decoration-oxblood",
      },
      size: {
        default: "h-9 px-4 has-[>svg]:px-3",
        sm: "h-9 px-3 text-[13px]",
        lg: "h-9 px-4",
        icon: "size-9 px-0",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean;
  }) {
  const Comp = asChild ? Slot : "button";

  return (
    <Comp
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  );
}

export { Button, buttonVariants };
