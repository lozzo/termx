"use client";

import { useState, useCallback } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog";
import { Plus, Loader2 } from "lucide-react";

interface SubItem {
  id: string;
  userId: string;
  username: string | null;
  email: string | null;
  planId: string;
  planName: string | null;
  status: string;
  currentPeriodEnd: string;
  createdAt: string;
}

export default function AdminSubscriptionsClient({ initialSubs }: { initialSubs: SubItem[] }) {
  const [subs, setSubs] = useState<SubItem[]>(initialSubs);
  const [createOpen, setCreateOpen] = useState(false);
  const [extendOpen, setExtendOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [selectedSub, setSelectedSub] = useState<SubItem | null>(null);

  const [formUserId, setFormUserId] = useState("");
  const [formPlanId, setFormPlanId] = useState("");
  const [formDays, setFormDays] = useState("30");
  const [extendDays, setExtendDays] = useState("30");

  const fetchSubs = useCallback(async () => {
    try {
      const res = await fetch("/api/admin/subscriptions");
      if (res.ok) setSubs(await res.json());
    } catch { /* */ }
  }, []);

  const handleCreate = async () => {
    if (!formUserId || !formPlanId || !formDays) return;
    setCreating(true);
    try {
      const res = await fetch("/api/admin/subscriptions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          userId: formUserId,
          planId: formPlanId,
          days: Number(formDays),
        }),
      });
      if (!res.ok) {
        const data = await res.json();
        alert(data.error || "创建失败");
        return;
      }
      setCreateOpen(false);
      setFormUserId("");
      setFormPlanId("");
      setFormDays("30");
      fetchSubs();
    } finally {
      setCreating(false);
    }
  };

  const handleExtend = async () => {
    if (!selectedSub || !extendDays) return;
    const res = await fetch(`/api/admin/subscriptions/${selectedSub.id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ extendDays: Number(extendDays) }),
    });
    if (!res.ok) {
      const data = await res.json();
      alert(data.error || "操作失败");
      return;
    }
    setExtendOpen(false);
    setSelectedSub(null);
    setExtendDays("30");
    fetchSubs();
  };

  const handleCancel = async (id: string) => {
    if (!confirm("确定取消该订阅？")) return;
    const res = await fetch(`/api/admin/subscriptions/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status: "cancelled" }),
    });
    if (!res.ok) {
      const data = await res.json();
      alert(data.error || "操作失败");
      return;
    }
    fetchSubs();
  };

  const statusMap: Record<string, { label: string; className: string }> = {
    active: { label: "活跃", className: "border-green-600 text-green-500" },
    cancelled: { label: "已取消", className: "border-red-600 text-red-500" },
    expired: { label: "已过期", className: "border-zinc-600 text-zinc-500" },
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-3xl font-bold tracking-tight">订阅管理</h2>
          <p className="text-zinc-400">查看和管理用户订阅。</p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>
          <Plus className="w-4 h-4 mr-2" />
          手动开通
        </Button>
      </div>

      <Card className="bg-zinc-900 border-zinc-800">
        <CardHeader>
          <CardTitle className="text-base">全部订阅 ({subs.length})</CardTitle>
        </CardHeader>
        <CardContent>
          {subs.length === 0 ? (
            <p className="text-center text-zinc-500 py-8">暂无订阅</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-800 text-zinc-400">
                    <th className="text-left py-3 px-2 font-medium">用户</th>
                    <th className="text-left py-3 px-2 font-medium">套餐</th>
                    <th className="text-left py-3 px-2 font-medium">状态</th>
                    <th className="text-left py-3 px-2 font-medium">到期时间</th>
                    <th className="text-left py-3 px-2 font-medium">创建时间</th>
                    <th className="text-right py-3 px-2 font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {subs.map((s) => {
                    const st = statusMap[s.status] || { label: s.status, className: "border-zinc-600 text-zinc-500" };
                    const isExpired = new Date(s.currentPeriodEnd) < new Date();
                    return (
                      <tr key={s.id} className="border-b border-zinc-800/50">
                        <td className="py-3 px-2">
                          <div className="text-white font-medium">{s.username || "-"}</div>
                          <div className="text-zinc-500 text-xs">{s.email}</div>
                        </td>
                        <td className="py-3 px-2 text-zinc-300">{s.planName || "-"}</td>
                        <td className="py-3 px-2">
                          <Badge variant="outline" className={isExpired && s.status === "active" ? "border-yellow-600 text-yellow-500" : st.className}>
                            {isExpired && s.status === "active" ? "已过期" : st.label}
                          </Badge>
                        </td>
                        <td className="py-3 px-2 text-zinc-400 text-xs">
                          {new Date(s.currentPeriodEnd).toLocaleDateString()}
                        </td>
                        <td className="py-3 px-2 text-zinc-400 text-xs">
                          {new Date(s.createdAt).toLocaleDateString()}
                        </td>
                        <td className="py-3 px-2 text-right space-x-1">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => { setSelectedSub(s); setExtendOpen(true); }}
                            className="text-xs"
                          >
                            延期
                          </Button>
                          {s.status === "active" && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleCancel(s.id)}
                              className="text-xs text-red-400 hover:text-red-300"
                            >
                              取消
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

      {/* 手动开通对话框 */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>手动开通订阅</DialogTitle>
            <DialogDescription>为指定用户手动开通订阅。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">用户 ID</label>
              <Input
                placeholder="用户 UUID"
                value={formUserId}
                onChange={(e) => setFormUserId(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">套餐 ID</label>
              <Input
                placeholder="套餐 UUID"
                value={formPlanId}
                onChange={(e) => setFormPlanId(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">天数</label>
              <Input
                type="number"
                placeholder="30"
                value={formDays}
                onChange={(e) => setFormDays(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
            <Button onClick={handleCreate} disabled={creating || !formUserId || !formPlanId}>
              {creating && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
              开通
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 延期对话框 */}
      <Dialog open={extendOpen} onOpenChange={setExtendOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>延长订阅</DialogTitle>
            <DialogDescription>
              为用户 {selectedSub?.username} 延长订阅时间。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">延长天数</label>
              <Input
                type="number"
                placeholder="30"
                value={extendDays}
                onChange={(e) => setExtendDays(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setExtendOpen(false)}>取消</Button>
            <Button onClick={handleExtend} disabled={!extendDays}>确认延期</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
