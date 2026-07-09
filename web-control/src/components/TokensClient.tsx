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
import { Loader2, Plus, Trash2, Copy, Check, Key } from "lucide-react";
import Link from "next/link";

interface TokenAgent {
  id: string;
  name: string;
  hostname: string;
  online: boolean;
}

interface TokenInfo {
  id: string;
  name: string;
  token: string;
  expiresAt: string | null;
  createdAt: string;
  agentCount: number;
  agents: TokenAgent[];
}

interface TokensClientProps {
  initialTokens: TokenInfo[];
  hasSubscription: boolean;
}

export default function TokensClient({ initialTokens, hasSubscription }: TokensClientProps) {
  const [tokens, setTokens] = useState<TokenInfo[]>(initialTokens);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [subscriptionDialogOpen, setSubscriptionDialogOpen] = useState(false);
  const [newTokenName, setNewTokenName] = useState("");
  const [newTokenExpiry, setNewTokenExpiry] = useState("90d");
  const [createLoading, setCreateLoading] = useState(false);
  const [createdToken, setCreatedToken] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [copiedTokenId, setCopiedTokenId] = useState<string | null>(null);

  const loadTokens = useCallback(() => {
    fetch("/api/tokens")
      .then((res) => res.json())
      .then(setTokens)
      .catch(console.error);
  }, []);

  const handleCreateToken = async () => {
    if (!newTokenName.trim()) return;
    setCreateLoading(true);
    try {
      const res = await fetch("/api/tokens", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: newTokenName.trim(), expires_in: newTokenExpiry }),
      });
      const data = await res.json();
      if (!res.ok) {
        if (data.error === "subscription_required" || data.error === "token_limit_exceeded") {
          setCreateDialogOpen(false);
          setSubscriptionDialogOpen(true);
          return;
        }
        throw new Error(data.error || data.message);
      }
      setCreatedToken(data.token);
      loadTokens();
    } catch (err) {
      console.error(err);
    } finally {
      setCreateLoading(false);
    }
  };

  const handleDeleteToken = async (id: string) => {
    if (!confirm("确定要删除该密钥？使用此密钥注册的节点不受影响，但无法再用此密钥注册新节点。")) return;
    await fetch(`/api/tokens/${id}`, { method: "DELETE" });
    loadTokens();
  };

  const handleCopyToken = () => {
    if (createdToken) {
      navigator.clipboard.writeText(createdToken);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const handleCopyExistingToken = (tokenId: string, tokenValue: string) => {
    navigator.clipboard.writeText(tokenValue);
    setCopiedTokenId(tokenId);
    setTimeout(() => setCopiedTokenId(null), 2000);
  };

  const closeCreateDialog = () => {
    setCreateDialogOpen(false);
    setNewTokenName("");
    setNewTokenExpiry("90d");
    setCreatedToken(null);
    setCopied(false);
  };

  const formatExpiry = (expiresAt: string | null) => {
    if (!expiresAt) return "永不过期";
    const d = new Date(expiresAt);
    if (d < new Date()) return "已过期";
    return d.toLocaleDateString();
  };

  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-3xl font-bold tracking-tight">连接密钥</h2>
        <p className="text-zinc-400">管理用于注册 TermX 节点的连接密钥。</p>
      </div>

      <Card className="bg-zinc-900 border-zinc-800">
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="flex items-center gap-2">
                <Key className="w-5 h-5" />
                密钥列表
              </CardTitle>
              <CardDescription>用于注册 TermX 节点的长效密钥。</CardDescription>
            </div>
            <Button
              onClick={() => {
                if (!hasSubscription) { setSubscriptionDialogOpen(true); return; }
                setCreateDialogOpen(true);
              }}
              className="bg-primary text-black hover:bg-primary/90"
              size="sm"
            >
              <Plus className="w-4 h-4 mr-1" />
              创建密钥
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {tokens.length === 0 ? (
            <div className="text-center py-8 text-zinc-500">
              <Key className="w-8 h-8 mx-auto mb-2 opacity-30" />
              <p className="text-sm">暂无密钥，创建一个用于注册节点</p>
            </div>
          ) : (
            <div className="space-y-3">
              {tokens.map((t) => (
                <div
                  key={t.id}
                  className="flex items-center justify-between p-3 rounded-lg border border-zinc-800 bg-zinc-950/50"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-white text-sm">{t.name}</span>
                    </div>
                    <div className="flex items-center gap-2 mt-1">
                      <code className="text-xs text-zinc-400 bg-black/50 px-2 py-0.5 rounded border border-zinc-800 truncate max-w-[300px]">
                        {t.token}
                      </code>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-zinc-400 hover:text-primary shrink-0 h-6 w-6"
                        onClick={() => handleCopyExistingToken(t.id, t.token)}
                      >
                        {copiedTokenId === t.id ? <Check className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
                      </Button>
                    </div>
                    <div className="flex items-center gap-3 text-xs text-zinc-500 mt-1">
                      <span>{formatExpiry(t.expiresAt)}</span>
                      <span>{t.agentCount} 个节点</span>
                      <span>创建于 {new Date(t.createdAt).toLocaleDateString()}</span>
                    </div>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="text-zinc-400 hover:text-red-500 shrink-0"
                    onClick={() => handleDeleteToken(t.id)}
                  >
                    <Trash2 className="w-4 h-4" />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* 创建密钥弹窗 */}
      <Dialog open={createDialogOpen} onOpenChange={(open) => { if (!open) closeCreateDialog(); }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{createdToken ? "密钥已创建" : "创建连接密钥"}</DialogTitle>
            <DialogDescription>
              {createdToken
                ? "密钥创建成功，您可以随时在密钥列表中查看和复制。"
                : "创建一个用于注册 TermX 节点的密钥。"}
            </DialogDescription>
          </DialogHeader>

          {createdToken ? (
            <div className="space-y-4 py-2">
              <div className="flex items-start gap-2">
                <code className="flex-1 text-xs text-green-300 bg-black/50 p-3 rounded border border-green-800/50 break-all select-all leading-relaxed">
                  {createdToken}
                </code>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={handleCopyToken}
                  className="text-green-400 shrink-0 mt-1"
                >
                  {copied ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                </Button>
              </div>
            </div>
          ) : (
            <div className="space-y-4 py-2">
              <div className="grid gap-2">
                <Label className="text-zinc-300">密钥名称</Label>
                <Input
                  placeholder="例如: 生产环境"
                  value={newTokenName}
                  onChange={(e) => setNewTokenName(e.target.value)}
                  className="bg-black border-zinc-800 text-white"
                />
              </div>
              <div className="grid gap-2">
                <Label className="text-zinc-300">有效期</Label>
                <select
                  value={newTokenExpiry}
                  onChange={(e) => setNewTokenExpiry(e.target.value)}
                  className="bg-black border border-zinc-800 text-white rounded-md px-3 py-2 text-sm"
                >
                  <option value="30d">30 天</option>
                  <option value="90d">90 天</option>
                  <option value="1y">1 年</option>
                  <option value="never">永不过期</option>
                </select>
              </div>
            </div>
          )}

          <DialogFooter>
            {createdToken ? (
              <Button onClick={closeCreateDialog} className="bg-white text-black hover:bg-zinc-200">
                完成
              </Button>
            ) : (
              <>
                <Button variant="ghost" onClick={closeCreateDialog} className="text-zinc-400">
                  取消
                </Button>
                <Button
                  onClick={handleCreateToken}
                  className="bg-white text-black hover:bg-zinc-200"
                  disabled={createLoading || !newTokenName.trim()}
                >
                  {createLoading ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : null}
                  创建
                </Button>
              </>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Subscription Required Dialog */}
      <Dialog open={subscriptionDialogOpen} onOpenChange={setSubscriptionDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>需要订阅</DialogTitle>
            <DialogDescription>
              创建连接密钥需要有效订阅。升级后即可创建密钥并注册节点。
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
