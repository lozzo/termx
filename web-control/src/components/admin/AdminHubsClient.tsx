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

interface HubItem {
  id: string;
  name: string;
  region: string;
  httpUrl: string;
  grpcUrl: string;
  status: string;
  agentCount: number;
  bandwidthMbps: number;
  cpuCores: number;
  memoryGb: number;
  maxAgents: number;
  lastHeartbeat: string | null;
  createdAt: string;
}

export default function AdminHubsClient({ initialHubs }: { initialHubs: HubItem[] }) {
  const [hubs, setHubs] = useState<HubItem[]>(initialHubs);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editHub, setEditHub] = useState<HubItem | null>(null);

  const [formName, setFormName] = useState("");
  const [formRegion, setFormRegion] = useState("");
  const [formHttpUrl, setFormHttpUrl] = useState("");
  const [formGrpcUrl, setFormGrpcUrl] = useState("");

  const fetchHubs = useCallback(async () => {
    try {
      const res = await fetch("/api/admin/hubs");
      if (res.ok) setHubs(await res.json());
    } catch { /* */ }
  }, []);

  const resetForm = () => {
    setFormName("");
    setFormRegion("");
    setFormHttpUrl("");
    setFormGrpcUrl("");
    setEditHub(null);
  };

  const openCreate = () => {
    resetForm();
    setDialogOpen(true);
  };

  const openEdit = (hub: HubItem) => {
    setEditHub(hub);
    setFormName(hub.name);
    setFormRegion(hub.region);
    setFormHttpUrl(hub.httpUrl);
    setFormGrpcUrl(hub.grpcUrl);
    setDialogOpen(true);
  };

  const handleSave = async () => {
    if (!formName) return;
    setSaving(true);
    try {
      const payload = {
        name: formName,
        region: formRegion,
        httpUrl: formHttpUrl,
        grpcUrl: formGrpcUrl,
      };

      const url = editHub ? `/api/admin/hubs/${editHub.id}` : "/api/admin/hubs";
      const method = editHub ? "PATCH" : "POST";

      const res = await fetch(url, {
        method,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });

      if (!res.ok) {
        const data = await res.json();
        alert(data.error || "操作失败");
        return;
      }

      setDialogOpen(false);
      resetForm();
      fetchHubs();
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (hub: HubItem) => {
    if (!confirm(`确定删除 Hub "${hub.name}"？`)) return;
    const res = await fetch(`/api/admin/hubs/${hub.id}`, { method: "DELETE" });
    if (!res.ok) {
      const data = await res.json();
      alert(data.error || "删除失败");
      return;
    }
    fetchHubs();
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-3xl font-bold tracking-tight">Hub 管理</h2>
          <p className="text-zinc-400">管理 Hub 中继节点。</p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="w-4 h-4 mr-2" />
          添加 Hub
        </Button>
      </div>

      <Card className="bg-zinc-900 border-zinc-800">
        <CardHeader>
          <CardTitle className="text-base">全部 Hub ({hubs.length})</CardTitle>
        </CardHeader>
        <CardContent>
          {hubs.length === 0 ? (
            <p className="text-center text-zinc-500 py-8">暂无 Hub</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-800 text-zinc-400">
                    <th className="text-left py-3 px-2 font-medium">名称</th>
                    <th className="text-left py-3 px-2 font-medium">地区</th>
                    <th className="text-left py-3 px-2 font-medium">HTTP URL</th>
                    <th className="text-left py-3 px-2 font-medium">状态</th>
                    <th className="text-left py-3 px-2 font-medium">节点数</th>
                    <th className="text-left py-3 px-2 font-medium">资源规格</th>
                    <th className="text-left py-3 px-2 font-medium">心跳</th>
                    <th className="text-right py-3 px-2 font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {hubs.map((h) => (
                    <tr key={h.id} className="border-b border-zinc-800/50">
                      <td className="py-3 px-2 font-medium text-white">{h.name}</td>
                      <td className="py-3 px-2 text-zinc-400">{h.region || "-"}</td>
                      <td className="py-3 px-2 text-zinc-400 text-xs font-mono">{h.httpUrl || "-"}</td>
                      <td className="py-3 px-2">
                        <Badge
                          variant="outline"
                          className={h.status === "online" ? "border-green-600 text-green-500" : "border-zinc-600 text-zinc-500"}
                        >
                          {h.status}
                        </Badge>
                      </td>
                      <td className="py-3 px-2 text-zinc-300">
                        {h.maxAgents > 0 ? `${h.agentCount}/${h.maxAgents}` : h.agentCount}
                      </td>
                      <td className="py-3 px-2 text-zinc-400 text-xs">
                        {h.bandwidthMbps || h.cpuCores || h.memoryGb ? (
                          <span>{h.bandwidthMbps}Mbps / {h.cpuCores}核 / {h.memoryGb}GB</span>
                        ) : (
                          <span className="text-zinc-600">未上报</span>
                        )}
                      </td>
                      <td className="py-3 px-2 text-zinc-400 text-xs">
                        {h.lastHeartbeat ? new Date(h.lastHeartbeat).toLocaleString() : "-"}
                      </td>
                      <td className="py-3 px-2 text-right space-x-1">
                        <Button variant="ghost" size="icon" onClick={() => openEdit(h)}>
                          <Pencil className="w-3.5 h-3.5" />
                        </Button>
                        <Button variant="ghost" size="icon" onClick={() => handleDelete(h)} className="text-red-400 hover:text-red-300">
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

      {/* 添加/编辑 Hub 对话框 */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editHub ? "编辑 Hub" : "添加 Hub"}</DialogTitle>
            <DialogDescription>{editHub ? "修改 Hub 信息。" : "创建新的 Hub 节点。"}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">名称</label>
              <Input
                placeholder="如 hub-cn-east"
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">地区</label>
              <Input
                placeholder="如 cn-east"
                value={formRegion}
                onChange={(e) => setFormRegion(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">HTTP URL</label>
              <Input
                placeholder="https://hub.example.com"
                value={formHttpUrl}
                onChange={(e) => setFormHttpUrl(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
            <div>
              <label className="text-sm text-zinc-400 mb-1 block">gRPC URL</label>
              <Input
                placeholder="grpc://hub.example.com:9090"
                value={formGrpcUrl}
                onChange={(e) => setFormGrpcUrl(e.target.value)}
                className="bg-zinc-800 border-zinc-700"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
            <Button onClick={handleSave} disabled={saving || !formName}>
              {saving && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
              {editHub ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
