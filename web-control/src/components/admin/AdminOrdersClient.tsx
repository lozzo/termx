"use client";

import { useState, useCallback } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { DollarSign, TrendingUp, ShoppingCart, CheckCircle } from "lucide-react";

interface OrderItem {
  id: string;
  userId: string;
  username: string | null;
  planName: string | null;
  amount: number;
  discountAmount: number;
  currency: string;
  billingCycle: string;
  status: string;
  paymentMethod: string;
  promoCode: string | null;
  createdAt: string;
  paidAt: string | null;
}

interface Stats {
  totalRevenue: number;
  monthlyRevenue: number;
  totalOrders: number;
  paidOrders: number;
}

interface AdminOrdersClientProps {
  initialOrders: OrderItem[];
  initialStats: Stats;
}

export default function AdminOrdersClient({ initialOrders, initialStats }: AdminOrdersClientProps) {
  const [orders, setOrders] = useState<OrderItem[]>(initialOrders);
  const [stats, setStats] = useState<Stats>(initialStats);

  const fetchOrders = useCallback(async () => {
    try {
      const res = await fetch("/api/admin/orders");
      if (res.ok) {
        const data = await res.json();
        setOrders(data.orders);
        setStats(data.stats);
      }
    } catch { /* */ }
  }, []);

  const handleMarkPaid = async (id: string) => {
    if (!confirm("确定标记为已支付？")) return;
    const res = await fetch(`/api/admin/orders/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status: "paid" }),
    });
    if (!res.ok) {
      const data = await res.json();
      alert(data.error || "操作失败");
      return;
    }
    fetchOrders();
  };

  const formatMoney = (amount: number) => `¥${(amount / 100).toFixed(2)}`;

  const statusMap: Record<string, { label: string; className: string }> = {
    pending: { label: "待支付", className: "border-yellow-600 text-yellow-500" },
    paid: { label: "已支付", className: "border-green-600 text-green-500" },
    expired: { label: "已过期", className: "border-zinc-600 text-zinc-500" },
    cancelled: { label: "已取消", className: "border-red-600 text-red-500" },
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-3xl font-bold tracking-tight">订单管理</h2>
        <p className="text-zinc-400">查看所有订单和收入统计。</p>
      </div>

      {/* 统计卡片 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <Card className="bg-zinc-900 border-zinc-800">
          <CardContent className="pt-6">
            <div className="flex items-center gap-2 text-zinc-400 text-sm">
              <DollarSign className="w-4 h-4" />
              总收入
            </div>
            <p className="text-2xl font-bold mt-1">{formatMoney(stats.totalRevenue)}</p>
          </CardContent>
        </Card>
        <Card className="bg-zinc-900 border-zinc-800">
          <CardContent className="pt-6">
            <div className="flex items-center gap-2 text-zinc-400 text-sm">
              <TrendingUp className="w-4 h-4" />
              本月收入
            </div>
            <p className="text-2xl font-bold mt-1">{formatMoney(stats.monthlyRevenue)}</p>
          </CardContent>
        </Card>
        <Card className="bg-zinc-900 border-zinc-800">
          <CardContent className="pt-6">
            <div className="flex items-center gap-2 text-zinc-400 text-sm">
              <ShoppingCart className="w-4 h-4" />
              总订单
            </div>
            <p className="text-2xl font-bold mt-1">{stats.totalOrders}</p>
          </CardContent>
        </Card>
        <Card className="bg-zinc-900 border-zinc-800">
          <CardContent className="pt-6">
            <div className="flex items-center gap-2 text-zinc-400 text-sm">
              <CheckCircle className="w-4 h-4" />
              已支付
            </div>
            <p className="text-2xl font-bold mt-1">{stats.paidOrders ?? 0}</p>
          </CardContent>
        </Card>
      </div>

      <Card className="bg-zinc-900 border-zinc-800">
        <CardHeader>
          <CardTitle className="text-base">全部订单</CardTitle>
        </CardHeader>
        <CardContent>
          {orders.length === 0 ? (
            <p className="text-center text-zinc-500 py-8">暂无订单</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-800 text-zinc-400">
                    <th className="text-left py-3 px-2 font-medium">订单号</th>
                    <th className="text-left py-3 px-2 font-medium">用户</th>
                    <th className="text-left py-3 px-2 font-medium">套餐</th>
                    <th className="text-left py-3 px-2 font-medium">金额</th>
                    <th className="text-left py-3 px-2 font-medium">优惠码</th>
                    <th className="text-left py-3 px-2 font-medium">状态</th>
                    <th className="text-left py-3 px-2 font-medium">时间</th>
                    <th className="text-right py-3 px-2 font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {orders.map((o) => {
                    const st = statusMap[o.status] || { label: o.status, className: "border-zinc-600 text-zinc-500" };
                    return (
                      <tr key={o.id} className="border-b border-zinc-800/50">
                        <td className="py-3 px-2 font-mono text-xs text-white">{o.id.slice(0, 8)}</td>
                        <td className="py-3 px-2 text-zinc-300">{o.username || "-"}</td>
                        <td className="py-3 px-2 text-zinc-400">{o.planName || "-"}</td>
                        <td className="py-3 px-2 text-zinc-300">
                          {formatMoney(o.amount)}
                          {o.discountAmount > 0 && (
                            <span className="text-green-500 text-xs ml-1">-{formatMoney(o.discountAmount)}</span>
                          )}
                        </td>
                        <td className="py-3 px-2 text-zinc-400 font-mono text-xs">{o.promoCode || "-"}</td>
                        <td className="py-3 px-2">
                          <Badge variant="outline" className={st.className}>{st.label}</Badge>
                        </td>
                        <td className="py-3 px-2 text-zinc-400 text-xs">
                          {new Date(o.createdAt).toLocaleDateString()}
                        </td>
                        <td className="py-3 px-2 text-right">
                          {o.status === "pending" && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleMarkPaid(o.id)}
                              className="text-xs"
                            >
                              标记已支付
                            </Button>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
