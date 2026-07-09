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
import { Loader2, Plus, Trash2, Pencil, FileCode } from "lucide-react";
import Link from "next/link";

interface SnippetInfo {
  id: string;
  title: string;
  content: string;
  createdAt: string;
}

interface SnippetsClientProps {
  initialSnippets: SnippetInfo[];
  hasSubscription: boolean;
}

export default function SnippetsClient({ initialSnippets, hasSubscription }: SnippetsClientProps) {
  const [snippets, setSnippets] = useState<SnippetInfo[]>(initialSnippets);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [subscriptionDialogOpen, setSubscriptionDialogOpen] = useState(false);
  const [dialogMode, setDialogMode] = useState<"add" | "edit">("add");
  const [editId, setEditId] = useState<string | null>(null);
  const [formTitle, setFormTitle] = useState("");
  const [formContent, setFormContent] = useState("");
  const [saving, setSaving] = useState(false);

  const loadSnippets = useCallback(async () => {
    try {
      const res = await fetch("/api/user/snippets");
      if (res.ok) {
        const data = await res.json();
        setSnippets(data);
      }
    } catch (err) {
      console.error(err);
    }
  }, []);

  const openAdd = () => {
    if (!hasSubscription) { setSubscriptionDialogOpen(true); return; }
    setDialogMode("add");
    setEditId(null);
    setFormTitle("");
    setFormContent("");
    setDialogOpen(true);
  };

  const openEdit = (snippet: SnippetInfo) => {
    setDialogMode("edit");
    setEditId(snippet.id);
    setFormTitle(snippet.title);
    setFormContent(snippet.content);
    setDialogOpen(true);
  };

  const handleSave = async () => {
    if (!formTitle.trim() || !formContent.trim()) return;
    setSaving(true);
    try {
      if (dialogMode === "add") {
        const res = await fetch("/api/user/snippets", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ title: formTitle.trim(), content: formContent.trim() }),
        });
        if (!res.ok) throw new Error("创建失败");
      } else if (editId) {
        const res = await fetch(`/api/user/snippets/${editId}`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ title: formTitle.trim(), content: formContent.trim() }),
        });
        if (!res.ok) throw new Error("更新失败");
      }
      setDialogOpen(false);
      await loadSnippets();
    } catch (err) {
      console.error(err);
      alert("操作失败，请重试");
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm("确定要删除这个代码片段？")) return;
    try {
      await fetch(`/api/user/snippets/${id}`, { method: "DELETE" });
      await loadSnippets();
    } catch (err) {
      console.error(err);
    }
  };

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-3xl font-bold tracking-tight">代码片段</h2>
          <p className="text-zinc-400">管理常用的代码片段，可在终端中快速发送。</p>
        </div>
        <Button onClick={openAdd} className="bg-primary text-black hover:bg-primary/90" size="sm">
          <Plus className="w-4 h-4 mr-1" />
          新建
        </Button>
      </div>

      {snippets.length === 0 ? (
        <Card className="bg-zinc-900 border-zinc-800">
          <CardContent className="py-12">
            <div className="text-center text-zinc-500">
              <FileCode className="w-10 h-10 mx-auto mb-3 opacity-30" />
              <p className="text-sm">暂无代码片段</p>
              <p className="text-xs mt-1">点击「新建」添加常用的代码片段</p>
            </div>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {snippets.map((snippet) => (
            <Card key={snippet.id} className="bg-zinc-900 border-zinc-800">
              <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-base truncate">{snippet.title}</CardTitle>
                  <div className="flex items-center gap-1 shrink-0">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 text-zinc-400 hover:text-white"
                      onClick={() => openEdit(snippet)}
                    >
                      <Pencil className="w-3.5 h-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 text-zinc-400 hover:text-red-500"
                      onClick={() => handleDelete(snippet.id)}
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </Button>
                  </div>
                </div>
                <CardDescription className="text-xs">
                  {new Date(snippet.createdAt).toLocaleDateString()}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <pre className="text-xs text-zinc-300 bg-black/50 rounded-lg p-3 border border-zinc-800 overflow-hidden max-h-32 font-mono whitespace-pre-wrap break-all">
                  {snippet.content}
                </pre>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {/* 添加/编辑 Dialog */}
      <Dialog open={dialogOpen} onOpenChange={(open) => { if (!open) setDialogOpen(false); }}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{dialogMode === "add" ? "新建代码片段" : "编辑代码片段"}</DialogTitle>
            <DialogDescription>
              {dialogMode === "add" ? "添加一个新的代码片段。" : "修改代码片段内容。"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="grid gap-2">
              <Label className="text-zinc-300">标题</Label>
              <Input
                placeholder="例如: Docker 启动命令"
                value={formTitle}
                onChange={(e) => setFormTitle(e.target.value)}
                className="bg-black border-zinc-800 text-white"
              />
            </div>
            <div className="grid gap-2">
              <Label className="text-zinc-300">内容</Label>
              <textarea
                placeholder="输入代码片段内容..."
                value={formContent}
                onChange={(e) => setFormContent(e.target.value)}
                rows={8}
                className="bg-black border border-zinc-800 text-white rounded-md px-3 py-2 text-sm font-mono resize-y focus:outline-none focus:ring-2 focus:ring-primary/50"
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
              disabled={saving || !formTitle.trim() || !formContent.trim()}
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
              创建代码片段需要有效订阅。升级后即可使用代码片段功能。
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