import { DocsLayout } from "fumadocs-ui/layouts/docs";
import type { ReactNode } from "react";
import { House } from "lucide-react";
import { source } from "@/lib/source";

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <DocsLayout
      tree={source.pageTree}
      nav={{
        url: "/",
        title: (
          <span className="inline-flex items-center gap-2">
            <span className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/10 bg-white/5 text-primary">
              <House className="h-4 w-4" />
            </span>
            <span className="text-sm font-semibold">返回首页</span>
          </span>
        ),
      }}
    >
      {children}
    </DocsLayout>
  );
}
