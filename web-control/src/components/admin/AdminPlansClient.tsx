"use client";

import { useState, useCallback } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog";
import { Loader2, Edit } from "lucide-react";

interface PlanItem {
  id: string;
  name: string;
  price: number;
  priceYearly: number;
  features: string;
  maxServers: number;
  maxAgents: number;
  active: boolean;
  createdAt: string;
}

export default function AdminPlansClient({ initialPlans }: { initialPlans: PlanItem[] }) {
  const [plans, setPlans] = useState<PlanItem[]>(initialPlans);
  const [editOpen, setEditOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editPlan, setEditPlan] = useState<PlanItem | null>(null);

  const [formName, setFormName] = useState("");
  const [formPrice, setFormPrice] = useState("");
  const [formPriceYearly, setFormPriceYearly] = useState("");
  const [formFeatures, setFormFeatures] = useState("");
  const [formMaxServers, setFormMaxServers] = useState("");
  const [formMaxAgents, setFormMaxAgents] = useState("");

  const fetchPlans = useCallback(async () => {
    try {
      const res = await fetch("/api/admin/plans");
      if (res.ok) setPlans(await res.json());
    } catch { /* */ }
  }, []);

  const openEdit = (plan: PlanItem) => {
    setEditPlan(plan);
    setFormName(plan.name);
    setFormPrice(String(plan.price));
    setFormPriceYearly(String(plan.priceYearly));
    setFormFeatures(plan.features);
    setFormMaxServers(String(plan.maxServers));
    setFormMaxAgents(String(plan.maxAgents));
    setEditOpen(true);
  };

  const handleSave = async () => {
    if (!editPlan) return;
    setSaving(true);
    try {
      const res = await fetch(`/api/admin/plans/${editPlan.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: formName,
          price: Number(formPrice),
          priceYearly: Number(formPriceYearly),
          features: formFeatures,
          maxServers: Number(formMaxServers),
          maxAgents: Number(formMaxAgents),
        }),
      });
      if (!res.ok) {
        const data = await res.json();
        alert(data.error || "保存失败");
        return;
      }
      setEditOpen(false);
      fetchPlans();
    } finally {
      setSaving(false);
    }
  };

  const handleToggleActive = async (plan: PlanItem) => {
    await fetch(`/api/admin/plans/${plan.id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ active: !plan.active }),
    });
    fetchPlans();
  };

  const formatMoney = (amount: number) => `¥${(amount / 100).toFixed(2)}`;

  const parseFeatures = (features: string): string[] => {
    try {
      return JSON.parse(features);
    } catch {
      return [];
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-3xl font-bold tracking-tight">套餐管理</h2>
        <p className="text-zinc-400">管理订阅套餐的价格和功能。</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {plans.map((plan) => (
          <Card key={plan.id} className={`bg-zinc-900 border-zinc-800 ${!plan.active ? "opacity-60" : ""}`}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-lg font-bold">{plan.name}</CardTitle>
              <div className="flex items-center gap-2">
                <Switch
                  checked={plan.active}
                  onCheckedChange={() => handleToggleActive(plan)}
                />
                <Button variant="ghost" size="icon" onClick={() => openEdit(plan)}>
                  <Edit className="w-4 h-4" />
                </Button>
              </div>
            </CardHeader>
            <CardContent className="space-y-3">
              <div className="flex items-baseline gap-2">
                <span className="text-2xl font-bold">{formatMoney(plan.price)}</span>
                <span className="text-zinc-500 text-sm">/月</span>
              </div>
              <div className="text-sm text-zinc-400">
                年付: {formatMoney(plan.priceYearly)}/年
              </div>
              <div className="flex gap-2 text-xs text-zinc-400">
                <Badge variant="outline" className="border-zinc-700">
                  {plan.maxServers} 服务器
                </Badge>
                <Badge variant="outline" className="border-zinc-700">
                  {plan.maxAgents} Agent
                </Badge>
              </div>
              <div className="pt-2 border-t border-zinc-800">
                <p className="text-xs text-zinc-500 mb-1">功能:</p>
                <ul className="text-xs text-zinc-400 space-y-1">
                  {parseFeatures(plan.features).map((f, i) => (
                    <li key={i}>· {f}</li>
                  ))}
                </ul>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* 编辑对话框 */}
      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑套餐</DialogTitle>
            <DialogDescription>修改套餐 {editPlan?.name} 的详细信息。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">名称</label>
              <Input
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-sm text-zinc-400 mb-1 block">月付价格（分）</label>
                <Input
                  type="number"
                  value={formPrice}
                  onChange={(e) => setFormPrice(e.target.value)}
                  className="bg-zinc-800 border-zinc-700"
                />
              </div>
              <div>
                <label className="text-sm text-zinc-400 mb-1 block">年付价格（分）</label>
                <Input
                  type="number"
                  value={formPriceYearly}
                  onChange={(e) => setFormPriceYearly(e.target.value)}
                  className="bg-zinc-800 border-zinc-700"
                />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-sm text-zinc-400 mb-1 block">最大服务器数</label>
                <Input
                  type="number"
                  value={formMaxServers}
                  onChange={(e) => setFormMaxServers(e.target.value)}
                  className="bg-zinc-800 border-zinc-700"
                />
              </div>
              <div>
                <label className="text-sm text-zinc-400 mb-1 block">最大 节点数</label>
                <Input
                  type="number"
                  value={formMaxAgents}
                  onChange={(e) => setFormMaxAgents(e.target.value)}
                  className="bg-zinc-800 border-zinc-700"
                />
              </div>
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">功能列表（JSON 数组）</label>
              <Input
                value={formFeatures}
                onChange={(e) => setFormFeatures(e.target.value)}
                placeholder='["功能1", "功能2"]'
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditOpen(false)}>取消</Button>
            <Button onClick={handleSave} disabled={saving}>
              {saving && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
