"use client";

import { useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  LayoutDashboard,
  Server,
  CreditCard,
  Settings,
  LogOut,
  Terminal,
  Menu,
  X,
  User,
  Tag,
  Users,
  ShoppingCart,
  KeyRound,
  Package,
  Globe,
  Monitor,
  Rocket,
  Key,
  Gift,
  Zap,
  FileCode,
  Bookmark,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const navItems = [
  { icon: LayoutDashboard, label: "概览", path: "/dashboard" },
  { icon: Server, label: "我的节点", path: "/dashboard/servers" },
  { icon: CreditCard, label: "账单与订阅", path: "/dashboard/billing" },
  { icon: Key, label: "连接密钥", path: "/dashboard/tokens" },
  { icon: Zap, label: "快捷键", path: "/dashboard/fn-config" },
  { icon: FileCode, label: "代码片段", path: "/dashboard/snippets" },
  { icon: Bookmark, label: "路径收藏", path: "/dashboard/path-bookmarks" },
  { icon: Gift, label: "邀请好友", path: "/dashboard/referral" },
  { icon: Settings, label: "账户设置", path: "/dashboard/settings" },
];

const adminNavItems = [
  { icon: Users, label: "用户管理", path: "/dashboard/admin/users" },
  { icon: ShoppingCart, label: "订单管理", path: "/dashboard/admin/orders" },
  { icon: KeyRound, label: "订阅管理", path: "/dashboard/admin/subscriptions" },
  { icon: Package, label: "套餐管理", path: "/dashboard/admin/plans" },
  { icon: Globe, label: "Hub 管理", path: "/dashboard/admin/hubs" },
  { icon: Monitor, label: "节点概览", path: "/dashboard/admin/agents" },
  { icon: Rocket, label: "版本管理", path: "/dashboard/admin/releases" },
  { icon: Tag, label: "优惠码管理", path: "/dashboard/admin/promo-codes" },
  { icon: Zap, label: "用户特权", path: "/dashboard/admin/overrides" },
];

interface DashboardShellProps {
  user: {
    id: string;
    username: string;
    email: string;
    role: string;
  };
  children: React.ReactNode;
}

export default function DashboardShell({ user, children }: DashboardShellProps) {
  const [isSidebarOpen, setIsSidebarOpen] = useState(true);
  const pathname = usePathname();
  const router = useRouter();

  const handleLogout = async () => {
    await fetch("/api/auth/logout", { method: "POST" });
    window.location.href = "/login";
  };

  return (
    <div className="min-h-screen bg-black text-white font-sans flex overflow-hidden relative">
      {/* Background Grid */}
      <div className="fixed inset-0 z-0 pointer-events-none">
        <div className="absolute inset-0 bg-[linear-gradient(to_right,#80808012_1px,transparent_1px),linear-gradient(to_bottom,#80808012_1px,transparent_1px)] bg-[size:24px_24px]"></div>
      </div>

      {/* Sidebar */}
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-50 w-64 bg-black/80 backdrop-blur-xl border-r border-white/5 transform transition-transform duration-300 ease-in-out lg:relative lg:translate-x-0",
          !isSidebarOpen && "-translate-x-full lg:hidden"
        )}
      >
        <div className="h-16 flex items-center px-6 border-b border-zinc-900">
          <Link
            href="/"
            className="flex items-center gap-2 font-mono font-bold text-xl tracking-tighter"
          >
            <Terminal className="w-6 h-6 text-primary" />
            <span>termx</span>
          </Link>
        </div>

        {/* User info */}
        <div className="px-4 py-3 border-b border-zinc-900">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-full bg-primary/20 flex items-center justify-center">
              <User className="w-4 h-4 text-primary" />
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-white truncate">
                {user.username}
              </p>
              <p className="text-xs text-zinc-500 truncate">{user.email}</p>
            </div>
          </div>
        </div>

        <nav className="p-4 space-y-2">
          {navItems.map((item) => {
            const isActive =
              pathname === item.path ||
              (item.path !== "/dashboard" && pathname.startsWith(item.path));
            return (
              <Link href={item.path} key={item.path}>
                <Button
                  variant="ghost"
                  className={cn(
                    "w-full justify-start gap-3 text-zinc-400 hover:text-white hover:bg-zinc-900",
                    isActive &&
                      "bg-zinc-900 text-primary hover:bg-zinc-900 hover:text-primary"
                  )}
                >
                  <item.icon className="w-5 h-5" />
                  {item.label}
                </Button>
              </Link>
            );
          })}

          {user.role === "admin" && (
            <>
              <div className="pt-3 pb-1 px-2">
                <span className="text-xs text-zinc-600 font-medium uppercase tracking-wider">
                  管理
                </span>
              </div>
              {adminNavItems.map((item) => {
                const isActive =
                  pathname === item.path || pathname.startsWith(item.path);
                return (
                  <Link href={item.path} key={item.path}>
                    <Button
                      variant="ghost"
                      className={cn(
                        "w-full justify-start gap-3 text-zinc-400 hover:text-white hover:bg-zinc-900",
                        isActive &&
                          "bg-zinc-900 text-primary hover:bg-zinc-900 hover:text-primary"
                      )}
                    >
                      <item.icon className="w-5 h-5" />
                      {item.label}
                    </Button>
                  </Link>
                );
              })}
            </>
          )}
        </nav>

        <div className="absolute bottom-4 left-4 right-4">
          <Button
            variant="ghost"
            className="w-full justify-start gap-3 text-zinc-500 hover:text-red-400 hover:bg-red-950/10"
            onClick={handleLogout}
          >
            <LogOut className="w-5 h-5" />
            退出登录
          </Button>
        </div>
      </aside>

      {/* Main Content */}
      <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
        {/* Mobile Header */}
        <header className="lg:hidden h-16 bg-black border-b border-zinc-900 flex items-center justify-between px-4">
          <div className="flex items-center gap-2 font-mono font-bold text-xl">
            <Terminal className="w-6 h-6 text-primary" />
            <span>termx</span>
          </div>
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setIsSidebarOpen(!isSidebarOpen)}
          >
            {isSidebarOpen ? (
              <X className="w-6 h-6" />
            ) : (
              <Menu className="w-6 h-6" />
            )}
          </Button>
        </header>

        <main className="flex-1 overflow-y-auto p-4 md:p-8 bg-transparent relative z-10">
          <div className="max-w-6xl mx-auto">{children}</div>
        </main>
      </div>
    </div>
  );
}
