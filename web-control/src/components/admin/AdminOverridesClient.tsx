"use client";

import { useState, useCallback } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog";
import { Plus, Pencil, Trash2, Loader2 } from "lucide-react";
import type { UserOverridesData } from "@/lib/schema";

interface OverrideItem {
  id: string;
  userId: string;
  username: string | null;
  email: string | null;
  overrides: UserOverridesData;
  note: string | null;
  expiresAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export default function AdminOverridesClient({
  initialOverrides,
}: {
  initialOverrides: OverrideItem[];
}) {
  const [items, setItems] = useState<OverrideItem[]>(initialOverrides);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<OverrideItem | null>(null);
  const [saving, setSaving] = useState(false);

  // 表单状态
  const [formUserId, setFormUserId] = useState("");
  const [formMaxServers, setFormMaxServers] = useState("");
  const [formMaxAgents, setFormMaxAgents] = useState("");
  const [formRelayBandwidthKbps, setFormRelayBandwidthKbps] = useState("");
  const [formAllowRelayTransfer, setFormAllowRelayTransfer] = useState(false);
  const [formNote, setFormNote] = useState("");
  const [formExpiresAt, setFormExpiresAt] = useState("");

  const fetchData = useCallback(async () => {
    try {
      const res = await fetch("/api/admin/overrides");
      if (res.ok) setItems(await res.json());
    } catch {
      /* */
    }
  }, []);

  const resetForm = () => {
    setFormUserId("");
    setFormMaxServers("");
    setFormMaxAgents("");
    setFormRelayBandwidthKbps("");
    setFormAllowRelayTransfer(false);
    setFormNote("");
    setFormExpiresAt("");
  };

  const openCreate = () => {
    setEditing(null);
    resetForm();
    setDialogOpen(true);
  };

  const openEdit = (item: OverrideItem) => {
    setEditing(item);
    setFormUserId(item.userId);
    setFormMaxServers(item.overrides.maxServers?.toString() ?? "");
    setFormMaxAgents(item.overrides.maxAgents?.toString() ?? "");
    setFormRelayBandwidthKbps(
      item.overrides.relayBandwidthKbps?.toString() ?? ""
    );
    setFormAllowRelayTransfer(item.overrides.allowRelayTransfer ?? false);
    setFormNote(item.note ?? "");
    setFormExpiresAt(
      item.expiresAt
        ? new Date(item.expiresAt).toISOString().slice(0, 16)
        : ""
    );
    setDialogOpen(true);
  };

  const buildOverrides = (): UserOverridesData => {
    const o: UserOverridesData = {};
    if (formMaxServers) o.maxServers = parseInt(formMaxServers);
    if (formMaxAgents) o.maxAgents = parseInt(formMaxAgents);
    if (formRelayBandwidthKbps)
      o.relayBandwidthKbps = parseInt(formRelayBandwidthKbps);
    if (formAllowRelayTransfer) o.allowRelayTransfer = true;
    return o;
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const payload = {
        userId: formUserId,
        overrides: buildOverrides(),
        note: formNote,
        expiresAt: formExpiresAt || null,
      };

      const res = await fetch(
        editing
          ? `/api/admin/overrides/${editing.id}`
          : "/api/admin/overrides",
        {
          method: editing ? "PATCH" : "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        }
      );

      if (!res.ok) {
        const data = await res.json();
        alert(data.error || "操作失败");
        return;
      }

      setDialogOpen(false);
      fetchData();
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (item: OverrideItem) => {
    if (!confirm(`确定删除用户 "${item.username}" 的特权配置？`)) return;
    const res = await fetch(`/api/admin/overrides/${item.id}`, {
      method: "DELETE",
    });
    if (!res.ok) {
      const data = await res.json();
      alert(data.error || "删除失败");
      return;
    }
    fetchData();
  };

  const isExpired = (expiresAt: string | null) => {
    if (!expiresAt) return false;
    return new Date(expiresAt) < new Date();
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-3xl font-bold tracking-tight">用户特权</h2>
        <p className="text-zinc-400">
          给特定用户设置高于套餐默认值的特权覆盖。
        </p>
      </div>

      <div className="flex items-center justify-between">
        <Button onClick={openCreate}>
          <Plus className="w-4 h-4 mr-2" />
          新增特权
        </Button>
      </div>

      <Card className="bg-zinc-900 border-zinc-800">
        <CardHeader>
          <CardTitle className="text-base">
            特权列表 ({items.length})
          </CardTitle>
        </CardHeader>
        <CardContent>
          {items.length === 0 ? (
            <p className="text-center text-zinc-500 py-8">暂无特权配置</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-800 text-zinc-400">
                    <th className="text-left py-3 px-2 font-medium">用户</th>
                    <th className="text-left py-3 px-2 font-medium">
                      覆盖项
                    </th>
                    <th className="text-left py-3 px-2 font-medium">备注</th>
                    <th className="text-left py-3 px-2 font-medium">状态</th>
                    <th className="text-left py-3 px-2 font-medium">
                      过期时间
                    </th>
                    <th className="text-right py-3 px-2 font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => (
                    <tr
                      key={item.id}
                      className="border-b border-zinc-800/50"
                    >
                      <td className="py-3 px-2">
                        <div className="font-medium text-white">
                          {item.username || "未知"}
                        </div>
                        <div className="text-xs text-zinc-500">
                          {item.email}
                        </div>
                      </td>
                      <td className="py-3 px-2">
                        <div className="flex flex-wrap gap-1">
                          {item.overrides.maxServers != null && (
                            <Badge variant="outline" className="border-zinc-700 text-zinc-300 text-xs">
                              服务器: {item.overrides.maxServers}
                            </Badge>
                          )}
                          {item.overrides.maxAgents != null && (
                            <Badge variant="outline" className="border-zinc-700 text-zinc-300 text-xs">
                              节点: {item.overrides.maxAgents}
                            </Badge>
                          )}
                          {item.overrides.relayBandwidthKbps != null && (
                            <Badge variant="outline" className="border-zinc-700 text-zinc-300 text-xs">
                              限速: {item.overrides.relayBandwidthKbps} KB/s
                            </Badge>
                          )}
                          {item.overrides.allowRelayTransfer && (
                            <Badge variant="outline" className="border-green-700 text-green-400 text-xs">
                              中继传文件
                            </Badge>
                          )}
                          {Object.keys(item.overrides).length === 0 && (
                            <span className="text-zinc-600">无</span>
                          )}
                        </div>
                      </td>
                      <td className="py-3 px-2 text-zinc-400 max-w-[200px] truncate">
                        {item.note || "-"}
                      </td>
                      <td className="py-3 px-2">
                        {isExpired(item.expiresAt) ? (
                          <Badge variant="outline" className="border-red-600 text-red-500">
                            已过期
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="border-green-600 text-green-500">
                            生效中
                          </Badge>
                        )}
                      </td>
                      <td className="py-3 px-2 text-zinc-400 text-xs">
                        {item.expiresAt
                          ? new Date(item.expiresAt).toLocaleString()
                          : "永不过期"}
                      </td>
                      <td className="py-3 px-2 text-right space-x-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => openEdit(item)}
                        >
                          <Pencil className="w-3.5 h-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-red-400 hover:text-red-300"
                          onClick={() => handleDelete(item)}
                        >
                          <Trash2 className="w-3.5 h-3.5" />
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

      {/* 新增/编辑对话框 */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editing ? "编辑用户特权" : "新增用户特权"}
            </DialogTitle>
            <DialogDescription>
              设置的值将覆盖该用户套餐的默认值。留空表示不覆盖。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">
                用户 ID
              </label>
              <Input
                placeholder="输入用户 ID"
                value={formUserId}
                onChange={(e) => setFormUserId(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
                disabled={!!editing}
              />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-sm text-zinc-400 mb-1 block">
                  最大服务器数
                </label>
                <Input
                  type="number"
                  placeholder="留空不覆盖"
                  value={formMaxServers}
                  onChange={(e) => setFormMaxServers(e.target.value)}
                  className="bg-zinc-800 border-zinc-700"
                />
              </div>
              <div>
                <label className="text-sm text-zinc-400 mb-1 block">
                  最大 节点数
                </label>
                <Input
                  type="number"
                  placeholder="留空不覆盖"
                  value={formMaxAgents}
                  onChange={(e) => setFormMaxAgents(e.target.value)}
                  className="bg-zinc-800 border-zinc-700"
                />
              </div>
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">
                中继限速 (KB/s)
              </label>
              <Input
                type="number"
                placeholder="0=不限，留空不覆盖"
                value={formRelayBandwidthKbps}
                onChange={(e) => setFormRelayBandwidthKbps(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div className="flex items-center justify-between">
              <label className="text-sm text-zinc-400">
                允许中继传文件
              </label>
              <Switch
                checked={formAllowRelayTransfer}
                onCheckedChange={setFormAllowRelayTransfer}
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">备注</label>
              <Input
                placeholder="可选备注"
                value={formNote}
                onChange={(e) => setFormNote(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">
                过期时间（留空=永不过期）
              </label>
              <Input
                type="datetime-local"
                value={formExpiresAt}
                onChange={(e) => setFormExpiresAt(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              取消
            </Button>
            <Button disabled={saving || !formUserId} onClick={handleSave}>
              {saving && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
              {editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
