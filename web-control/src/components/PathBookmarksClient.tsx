"use client";

import { useState, useCallback } from "react";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Loader2, Plus, Trash2, Pencil, Bookmark } from "lucide-react";
import Link from "next/link";

interface PathBookmarkInfo {
  id: string;
  agentId: string;
  path: string;
  name: string;
  createdAt: string;
}

interface AgentInfo {
  id: string;
  name: string;
  hostname: string;
}

interface PathBookmarksClientProps {
  initialBookmarks: PathBookmarkInfo[];
  agents: AgentInfo[];
  hasSubscription: boolean;
}

export default function PathBookmarksClient({ initialBookmarks, agents, hasSubscription }: PathBookmarksClientProps) {
  const [bookmarks, setBookmarks] = useState<PathBookmarkInfo[]>(initialBookmarks);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [subscriptionDialogOpen, setSubscriptionDialogOpen] = useState(false);
  const [dialogMode, setDialogMode] = useState<"add" | "edit">("add");
  const [editId, setEditId] = useState<string | null>(null);
  const [formName, setFormName] = useState("");
  const [formPath, setFormPath] = useState("");
  const [formAgentId, setFormAgentId] = useState("");
  const [saving, setSaving] = useState(false);

  const loadBookmarks = useCallback(async () => {
    try {
      const res = await fetch("/api/user/path-bookmarks");
      if (res.ok) {
        const data = await res.json();
        setBookmarks(data);
      }
    } catch (err) {
      console.error(err);
    }
  }, []);

  const getAgentLabel = (agentId: string) => {
    const agent = agents.find(a => a.id === agentId);
    return agent ? (agent.name || agent.hostname || agentId.slice(0, 8)) : agentId.slice(0, 8);
  };

  const openAdd = () => {
    if (!hasSubscription) { setSubscriptionDialogOpen(true); return; }
    setDialogMode("add");
    setEditId(null);
    setFormName("");
    setFormPath("");
    setFormAgentId(agents[0]?.id || "");
    setDialogOpen(true);
  };

  const openEdit = (bookmark: PathBookmarkInfo) => {
    setDialogMode("edit");
    setEditId(bookmark.id);
    setFormName(bookmark.name);
    setFormPath(bookmark.path);
    setFormAgentId(bookmark.agentId);
    setDialogOpen(true);
  };

  const handleSave = async () => {
    if (!formName.trim() || !formPath.trim()) return;
    setSaving(true);
    try {
      if (dialogMode === "add") {
        if (!formAgentId) { alert("请选择服务器"); setSaving(false); return; }
        const res = await fetch("/api/user/path-bookmarks", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ name: formName.trim(), path: formPath.trim(), agentId: formAgentId }),
        });
        if (!res.ok) throw new Error("创建失败");
      } else if (editId) {
        const res = await fetch(`/api/user/path-bookmarks/${editId}`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ name: formName.trim(), path: formPath.trim() }),
        });
        if (!res.ok) throw new Error("更新失败");
      }
      setDialogOpen(false);
      await loadBookmarks();
    } catch (err) {
      console.error(err);
      alert("操作失败，请重试");
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm("确定要删除这个路径收藏？")) return;
    try {
      await fetch(`/api/user/path-bookmarks/${id}`, { method: "DELETE" });
      await loadBookmarks();
    } catch (err) {
      console.error(err);
    }
  };

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-3xl font-bold tracking-tight">路径收藏</h2>
          <p className="text-zinc-400">管理常用的文件路径，可在文件管理器中快速跳转。</p>
        </div>
        <Button onClick={openAdd} className="bg-primary text-black hover:bg-primary/90" size="sm">
          <Plus className="w-4 h-4 mr-1" />
          新建
        </Button>
      </div>

      {bookmarks.length === 0 ? (
        <Card className="bg-zinc-900 border-zinc-800">
          <CardContent className="py-12">
            <div className="text-center text-zinc-500">
              <Bookmark className="w-10 h-10 mx-auto mb-3 opacity-30" />
              <p className="text-sm">暂无路径收藏</p>
              <p className="text-xs mt-1">点击「新建」添加常用的文件路径</p>
            </div>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {bookmarks.map((bookmark) => (
            <Card key={bookmark.id} className="bg-zinc-900 border-zinc-800">
              <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-base truncate">{bookmark.name}</CardTitle>
                  <div className="flex items-center gap-1 shrink-0">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 text-zinc-400 hover:text-white"
                      onClick={() => openEdit(bookmark)}
                    >
                      <Pencil className="w-3.5 h-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 text-zinc-400 hover:text-red-500"
                      onClick={() => handleDelete(bookmark.id)}
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </Button>
                  </div>
                </div>
                <CardDescription className="text-xs flex items-center gap-2">
                  <span className="bg-zinc-800 px-1.5 py-0.5 rounded text-zinc-400">{getAgentLabel(bookmark.agentId)}</span>
                  <span>{new Date(bookmark.createdAt).toLocaleDateString()}</span>
                </CardDescription>
              </CardHeader>
              <CardContent>
                <code className="text-xs text-zinc-300 bg-black/50 rounded-lg px-3 py-2 border border-zinc-800 block truncate font-mono">
                  {bookmark.path}
                </code>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {/* 添加/编辑 Dialog */}
      <Dialog open={dialogOpen} onOpenChange={(open) => { if (!open) setDialogOpen(false); }}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{dialogMode === "add" ? "新建路径收藏" : "编辑路径收藏"}</DialogTitle>
            <DialogDescription>
              {dialogMode === "add" ? "添加一个常用的文件路径。" : "修改路径收藏信息。"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            {dialogMode === "add" && (
              <div className="grid gap-2">
                <Label className="text-zinc-300">服务器</Label>
                <select
                  value={formAgentId}
                  onChange={(e) => setFormAgentId(e.target.value)}
                  className="bg-black border border-zinc-800 text-white rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary/50"
                >
                  {agents.length === 0 && <option value="">无可用服务器</option>}
                  {agents.map(a => (
                    <option key={a.id} value={a.id}>{a.name || a.hostname || a.id.slice(0, 8)}</option>
                  ))}
                </select>
              </div>
            )}
            <div className="grid gap-2">
              <Label className="text-zinc-300">名称</Label>
              <Input
                placeholder="例如: 项目目录"
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                className="bg-black border-zinc-800 text-white"
              />
            </div>
            <div className="grid gap-2">
              <Label className="text-zinc-300">路径</Label>
              <Input
                placeholder="例如: /home/user/project"
                value={formPath}
                onChange={(e) => setFormPath(e.target.value)}
                className="bg-black border-zinc-800 text-white font-mono"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDialogOpen(false)} className="text-zinc-400">
              取消
            </Button>
            <Button
              onClick={handleSave}
              className="bg-white text-black hover:bg-zinc-200"
              disabled={saving || !formName.trim() || !formPath.trim()}
            >
              {saving ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : null}
              {dialogMode === "add" ? "创建" : "保存"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Subscription Required Dialog */}
      <Dialog open={subscriptionDialogOpen} onOpenChange={setSubscriptionDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>需要订阅</DialogTitle>
            <DialogDescription>
              创建路径收藏需要有效订阅。升级后即可使用路径收藏功能。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setSubscriptionDialogOpen(false)} className="text-zinc-400">
              取消
            </Button>
            <Link href="/dashboard/billing">
              <Button className="bg-primary text-black">查看订阅计划</Button>
            </Link>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}