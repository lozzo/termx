import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex min-h-11 items-center justify-center gap-2 border text-[10px] font-semibold uppercase transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary disabled:pointer-events-none disabled:opacity-45 [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "border-primary bg-primary px-4 text-primary-foreground hover:brightness-95",
        outline: "border-line-strong bg-panel px-4 text-foreground hover:border-foreground hover:bg-soft",
        ghost: "border-transparent bg-transparent px-3 text-muted-foreground hover:bg-soft hover:text-foreground",
        destructive: "border-destructive bg-transparent px-3 text-destructive hover:bg-destructive/10",
      },
      size: { default: "h-11", sm: "h-9 min-h-9 px-3", icon: "size-11 min-h-11 p-0" },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(({ className, variant, size, ...props }, ref) => (
  <button ref={ref} className={cn(buttonVariants({ variant, size, className }))} {...props} />
));
Button.displayName = "Button";

export { buttonVariants };
