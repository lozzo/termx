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
import { Plus, Loader2, Pencil, Trash2 } from "lucide-react";

interface PromoCode {
  id: string;
  code: string;
  discountType: string;
  discountValue: number;
  maxUses: number | null;
  usedCount: number;
  startsAt: string | null;
  expiresAt: string | null;
  active: boolean;
  createdAt: string;
}

export default function AdminPromoCodesClient({ initialCodes }: { initialCodes: PromoCode[] }) {
  const [codes, setCodes] = useState<PromoCode[]>(initialCodes);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [editingCodeId, setEditingCodeId] = useState<string | null>(null);

  // 创建表单
  const [formCode, setFormCode] = useState("");
  const [formType, setFormType] = useState<"fixed" | "percent">("fixed");
  const [formValue, setFormValue] = useState("");
  const [formMaxUses, setFormMaxUses] = useState("");
  const [formStartsAt, setFormStartsAt] = useState("");
  const [formExpiresAt, setFormExpiresAt] = useState("");
  const [formActive, setFormActive] = useState(true);

  const resetForm = useCallback(() => {
    setFormCode("");
    setFormType("fixed");
    setFormValue("");
    setFormMaxUses("");
    setFormStartsAt("");
    setFormExpiresAt("");
    setFormActive(true);
    setEditingCodeId(null);
  }, []);

  const formatDateTimeLocal = (value: string | null) => {
    if (!value) return "";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "";
    const pad = (num: number) => String(num).padStart(2, "0");
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
  };

  const openCreateDialog = () => {
    resetForm();
    setCreateDialogOpen(true);
  };

  const openEditDialog = (code: PromoCode) => {
    setEditingCodeId(code.id);
    setFormCode(code.code);
    setFormType(code.discountType as "fixed" | "percent");
    setFormValue(String(code.discountValue));
    setFormMaxUses(code.maxUses === null ? "" : String(code.maxUses));
    setFormStartsAt(formatDateTimeLocal(code.startsAt));
    setFormExpiresAt(formatDateTimeLocal(code.expiresAt));
    setFormActive(code.active);
    setEditDialogOpen(true);
  };

  const buildPayload = () => {
    const body: Record<string, unknown> = {
      code: formCode,
      discountType: formType,
      discountValue: Number(formValue),
      active: formActive,
      maxUses: formMaxUses ? Number(formMaxUses) : null,
      startsAt: formStartsAt || null,
      expiresAt: formExpiresAt || null,
    };

    return body;
  };

  const fetchCodes = useCallback(async () => {
    try {
      const res = await fetch("/api/admin/promo-codes");
      if (res.ok) setCodes(await res.json());
    } catch { /* */ }
  }, []);

  const handleCreate = async () => {
    if (!formCode || !formValue) return;
    setSubmitting(true);
    try {
      const res = await fetch("/api/admin/promo-codes", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(buildPayload()),
      });

      if (!res.ok) {
        const data = await res.json();
        alert(data.error || "创建失败");
        return;
      }

      setCreateDialogOpen(false);
      resetForm();
      await fetchCodes();
    } finally {
      setSubmitting(false);
    }
  };

  const handleEdit = async () => {
    if (!editingCodeId || !formCode || !formValue) return;
    setSubmitting(true);
    try {
      const res = await fetch(`/api/admin/promo-codes/${editingCodeId}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(buildPayload()),
      });

      if (!res.ok) {
        const data = await res.json();
        alert(data.error || "更新失败");
        return;
      }

      setEditDialogOpen(false);
      resetForm();
      await fetchCodes();
    } finally {
      setSubmitting(false);
    }
  };

  const handleToggle = async (id: string, active: boolean) => {
    await fetch(`/api/admin/promo-codes/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ active: !active }),
    });
    fetchCodes();
  };

  const handleDelete = async (code: PromoCode) => {
    const confirmed = window.confirm(
      code.usedCount > 0
        ? `优惠码 ${code.code} 已有使用记录，删除将改为停用。继续吗？`
        : `确认删除优惠码 ${code.code} 吗？未使用的优惠码会被永久删除。`
    );

    if (!confirmed) return;

    const res = await fetch(`/api/admin/promo-codes/${code.id}`, {
      method: "DELETE",
    });

    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      alert(data.error || "删除失败");
      return;
    }

    if (data.deactivated) {
      alert("该优惠码已有关联记录，已自动停用。");
    }

    fetchCodes();
  };

  const formatDiscount = (type: string, value: number) => {
    return type === "fixed" ? `¥${(value / 100).toFixed(2)}` : `${value}%`;
  };

  const selectedCode = editingCodeId
    ? codes.find((item) => item.id === editingCodeId) || null
    : null;
  const immutableFields = (selectedCode?.usedCount || 0) > 0;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-3xl font-bold tracking-tight">优惠码管理</h2>
          <p className="text-zinc-400">创建和管理促销优惠码。</p>
        </div>
        <Button onClick={openCreateDialog}>
          <Plus className="w-4 h-4 mr-2" />
          创建优惠码
        </Button>
      </div>

      <Card className="bg-zinc-900 border-zinc-800">
        <CardHeader>
          <CardTitle className="text-base">全部优惠码</CardTitle>
        </CardHeader>
        <CardContent>
          {codes.length === 0 ? (
            <p className="text-center text-zinc-500 py-8">暂无优惠码</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-800 text-zinc-400">
                    <th className="text-left py-3 px-2 font-medium">优惠码</th>
                    <th className="text-left py-3 px-2 font-medium">类型</th>
                    <th className="text-left py-3 px-2 font-medium">折扣</th>
                    <th className="text-left py-3 px-2 font-medium">使用量</th>
                    <th className="text-left py-3 px-2 font-medium">有效期</th>
                    <th className="text-left py-3 px-2 font-medium">状态</th>
                    <th className="text-right py-3 px-2 font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {codes.map((c) => (
                    <tr key={c.id} className="border-b border-zinc-800/50">
                      <td className="py-3 px-2 font-mono font-semibold text-white">{c.code}</td>
                      <td className="py-3 px-2 text-zinc-400">
                        {c.discountType === "fixed" ? "固定金额" : "百分比"}
                      </td>
                      <td className="py-3 px-2 text-zinc-300">{formatDiscount(c.discountType, c.discountValue)}</td>
                      <td className="py-3 px-2 text-zinc-400">
                        {c.usedCount}{c.maxUses !== null ? ` / ${c.maxUses}` : " / 无限"}
                      </td>
                      <td className="py-3 px-2 text-zinc-400 text-xs">
                        {c.expiresAt ? new Date(c.expiresAt).toLocaleDateString() : "永不过期"}
                      </td>
                      <td className="py-3 px-2">
                        <Badge
                          variant="outline"
                          className={c.active ? "border-primary text-primary" : "border-zinc-600 text-zinc-500"}
                        >
                          {c.active ? "启用" : "停用"}
                        </Badge>
                      </td>
                      <td className="py-3 px-2 text-right">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => openEditDialog(c)}
                          className="text-xs"
                        >
                          <Pencil className="w-3.5 h-3.5 mr-1" />
                          编辑
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleToggle(c.id, c.active)}
                          className="text-xs"
                        >
                          {c.active ? "停用" : "启用"}
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDelete(c)}
                          className="text-xs text-red-400 hover:text-red-300"
                        >
                          <Trash2 className="w-3.5 h-3.5 mr-1" />
                          删除
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      {/* 创建优惠码对话框 */}
      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>创建优惠码</DialogTitle>
            <DialogDescription>设置优惠码的基本信息和使用限制。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">优惠码</label>
              <Input
                placeholder="如 WELCOME50"
                value={formCode}
                onChange={(e) => setFormCode(e.target.value.toUpperCase())}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">折扣类型</label>
              <div className="flex gap-2">
                <Button
                  variant={formType === "fixed" ? "default" : "outline"}
                  size="sm"
                  onClick={() => setFormType("fixed")}
                >
                  固定金额（分）
                </Button>
                <Button
                  variant={formType === "percent" ? "default" : "outline"}
                  size="sm"
                  onClick={() => setFormType("percent")}
                >
                  百分比
                </Button>
              </div>
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">
                折扣值 {formType === "fixed" ? "（单位：分，如 500 = ¥5）" : "（1-100）"}
              </label>
              <Input
                type="number"
                placeholder={formType === "fixed" ? "500" : "50"}
                value={formValue}
                onChange={(e) => setFormValue(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">最大使用次数（留空不限）</label>
              <Input
                type="number"
                placeholder="100"
                value={formMaxUses}
                onChange={(e) => setFormMaxUses(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">开始时间（留空立即生效）</label>
              <Input
                type="datetime-local"
                value={formStartsAt}
                onChange={(e) => setFormStartsAt(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">过期时间（留空不过期）</label>
              <Input
                type="datetime-local"
                value={formExpiresAt}
                onChange={(e) => setFormExpiresAt(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>
              取消
            </Button>
            <Button onClick={handleCreate} disabled={submitting || !formCode || !formValue}>
              {submitting && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑优惠码</DialogTitle>
            <DialogDescription>
              {immutableFields ? "该优惠码已有使用记录，代码和折扣信息已锁定，只能调整状态和使用限制。" : "修改优惠码配置。"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">优惠码</label>
              <Input
                placeholder="如 WELCOME50"
                value={formCode}
                onChange={(e) => setFormCode(e.target.value.toUpperCase())}
                className="bg-zinc-800 border-zinc-700"
                disabled={immutableFields}
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">折扣类型</label>
              <div className="flex gap-2">
                <Button
                  variant={formType === "fixed" ? "default" : "outline"}
                  size="sm"
                  onClick={() => setFormType("fixed")}
                  disabled={immutableFields}
                >
                  固定金额（分）
                </Button>
                <Button
                  variant={formType === "percent" ? "default" : "outline"}
                  size="sm"
                  onClick={() => setFormType("percent")}
                  disabled={immutableFields}
                >
                  百分比
                </Button>
              </div>
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">
                折扣值 {formType === "fixed" ? "（单位：分，如 500 = ¥5）" : "（1-100）"}
              </label>
              <Input
                type="number"
                placeholder={formType === "fixed" ? "500" : "50"}
                value={formValue}
                onChange={(e) => setFormValue(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
                disabled={immutableFields}
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">最大使用次数（留空不限）</label>
              <Input
                type="number"
                placeholder="100"
                value={formMaxUses}
                onChange={(e) => setFormMaxUses(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">开始时间（留空立即生效）</label>
              <Input
                type="datetime-local"
                value={formStartsAt}
                onChange={(e) => setFormStartsAt(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">过期时间（留空不过期）</label>
              <Input
                type="datetime-local"
                value={formExpiresAt}
                onChange={(e) => setFormExpiresAt(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">状态</label>
              <div className="flex gap-2">
                <Button
                  variant={formActive ? "default" : "outline"}
                  size="sm"
                  onClick={() => setFormActive(true)}
                >
                  启用
                </Button>
                <Button
                  variant={!formActive ? "default" : "outline"}
                  size="sm"
                  onClick={() => setFormActive(false)}
                >
                  停用
                </Button>
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditDialogOpen(false)}>
              取消
            </Button>
            <Button onClick={handleEdit} disabled={submitting || !formCode || !formValue}>
              {submitting && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
