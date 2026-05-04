"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Activity, Server, Shield, Zap } from "lucide-react";
import Link from "next/link";

interface DashboardOverviewProps {
  user: { username: string };
  stats: { totalAgents: number; onlineAgents: number };
  subscription: {
    id: string;
    planId: string;
    planName: string;
    currentPeriodEnd: string;
    billingCycle: string;
  } | null;
}

export default function DashboardOverview({
  user,
  stats,
  subscription,
}: DashboardOverviewProps) {
  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-3xl font-bold tracking-tight">概览</h2>
        <p className="text-zinc-400">欢迎回来，{user.username}。</p>
      </div>

      {/* 未订阅提示横幅 */}
      {!subscription && (
        <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/5 p-4 flex items-center justify-between">
          <div>
            <p className="text-sm font-medium text-yellow-200">
              您当前使用免费版
            </p>
            <p className="text-xs text-zinc-400 mt-1">
              升级后可创建连接密钥，并通过 Hub 远程访问和管理多台服务器
            </p>
          </div>
          <Link href="/dashboard/billing">
            <Button
              size="sm"
              className="bg-yellow-500 text-black hover:bg-yellow-400"
            >
              升级
            </Button>
          </Link>
        </div>
      )}

      {subscription?.planId === "dev" && (
        <div className="rounded-lg border border-primary/30 bg-primary/5 p-4">
          <p className="text-sm font-medium text-primary">开发模式已启用</p>
          <p className="text-xs text-zinc-400 mt-1">
            支付和订阅校验已跳过，可以直接创建连接密钥、注册节点并使用远程连接功能。
          </p>
        </div>
      )}

      {/* 订阅即将到期提醒 */}
      {subscription && (() => {
        const daysLeft = Math.ceil((new Date(subscription.currentPeriodEnd).getTime() - Date.now()) / 86400000)
        return daysLeft <= 7 ? (
          <div className="rounded-lg border border-orange-500/30 bg-orange-500/5 p-4 flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-orange-200">
                您的订阅将于 {daysLeft <= 0 ? '今天' : `${daysLeft} 天后`}到期，请及时续费
              </p>
              <p className="text-xs text-zinc-400 mt-1">
                到期后将无法使用 Hub 远程连接和管理功能
              </p>
            </div>
            <Link href="/dashboard/billing">
              <Button
                size="sm"
                className="bg-orange-500 text-black hover:bg-orange-400"
              >
                续费
              </Button>
            </Link>
          </div>
        ) : null
      })()}

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <StatsCard
          title="节点总数"
          value={String(stats.totalAgents)}
          icon={<Server className="h-4 w-4 text-zinc-400" />}
          description="已注册 节点数量"
        />
        <StatsCard
          title="在线节点"
          value={String(stats.onlineAgents)}
          icon={<Activity className="h-4 w-4 text-zinc-400" />}
          description="当前在线节点"
        />
        <StatsCard
          title="数据传输"
          value="—"
          icon={<Zap className="h-4 w-4 text-zinc-400" />}
          description="本计费周期"
        />
        <StatsCard
          title="安全状态"
          value="安全"
          icon={<Shield className="h-4 w-4 text-green-500" />}
          description="未检测到异常"
        />
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-7">
        <Card className="col-span-4 bg-zinc-900 border-zinc-800">
          <CardHeader>
            <CardTitle>近期活动</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-8">
              <div className="flex items-center justify-center py-8">
                <p className="text-sm text-zinc-500">暂无近期活动</p>
              </div>
            </div>
          </CardContent>
        </Card>
        <Card className="col-span-3 bg-zinc-900 border-zinc-800">
          <CardHeader>
            <CardTitle>快捷操作</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <div className="p-4 border border-zinc-800 rounded-lg bg-black/50">
              <h4 className="text-sm font-medium text-zinc-300 mb-1">
                添加新节点
              </h4>
              <p className="text-xs text-zinc-500 mb-2">
                在目标机器上运行启动命令，用浏览器完成登录，然后用 Android 客户端扫码配对。
              </p>
              <code className="text-xs text-green-500 block bg-black p-2 rounded border border-zinc-800">
                termx remote enable --server &lt;control_url&gt;
              </code>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function StatsCard({
  title,
  value,
  icon,
  description,
}: {
  title: string;
  value: string;
  icon: React.ReactNode;
  description: string;
}) {
  return (
    <Card className="bg-zinc-900 border-zinc-800">
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-zinc-200">
          {title}
        </CardTitle>
        {icon}
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold text-white">{value}</div>
        <p className="text-xs text-zinc-500">{description}</p>
      </CardContent>
    </Card>
  );
}
