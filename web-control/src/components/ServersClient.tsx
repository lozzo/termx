"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  Plus,
  Trash2,
  Copy,
  Check,
  Server as ServerIcon,
  Terminal,
  QrCode,
  Key,
  Search,
  Loader2,
  Pencil,
  X,
  Monitor,
  Clock,
  Globe,
} from "lucide-react";
import Link from "next/link";

// --- Types ---

interface AgentInfo {
  id: string;
  name: string;
  hostname: string;
  osInfo: string | null;
  labels: string | null;
  online: boolean;
  paired: boolean;
  hubHttpUrl: string | null;
  tokenId: string;
  tokenName: string | null;
  lastSeen: string | null;
  createdAt: string;
}

type StatusFilter = "all" | "online" | "offline" | "unpaired";

interface ToastItem {
  id: number;
  message: string;
  type: "success" | "error";
}

// --- Helpers ---

function formatRelativeTime(dateStr: string): string {
  const now = Date.now();
  const then = new Date(dateStr).getTime();
  const diffSec = Math.floor((now - then) / 1000);
  if (diffSec < 60) return "刚刚";
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin} 分钟前`;
  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour} 小时前`;
  const diffDay = Math.floor(diffHour / 24);
  return `${diffDay} 天前`;
}

// --- StatsCard ---

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

// --- Toast Container ---

function ToastContainer({ toasts, onDismiss }: { toasts: ToastItem[]; onDismiss: (id: number) => void }) {
  if (toasts.length === 0) return null;
  return (
    <div className="fixed bottom-6 right-6 z-50 flex flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`flex items-center gap-2 px-4 py-3 rounded-lg text-sm shadow-lg border ${
            t.type === "success"
              ? "bg-green-500/10 text-green-400 border-green-500/20"
              : "bg-red-500/10 text-red-400 border-red-500/20"
          }`}
        >
          {t.type === "success" ? (
            <Check className="w-4 h-4 shrink-0" />
          ) : (
            <X className="w-4 h-4 shrink-0" />
          )}
          <span>{t.message}</span>
          <button onClick={() => onDismiss(t.id)} className="ml-2 opacity-60 hover:opacity-100">
            <X className="w-3 h-3" />
          </button>
        </div>
      ))}
    </div>
  );
}

// --- Main Component ---

interface ServersClientProps {
  initialAgents: AgentInfo[];
  hasSubscription: boolean;
}

export default function ServersClient({ initialAgents, hasSubscription: initialHasSubscription }: ServersClientProps) {
  const [agents, setAgents] = useState<AgentInfo[]>(initialAgents);
  const [loading, setLoading] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [subscriptionDialogOpen, setSubscriptionDialogOpen] = useState(false);
  const [hasSubscription] = useState<boolean>(initialHasSubscription);
  const [copied, setCopied] = useState<string | null>(null);
  const [showCmdFor, setShowCmdFor] = useState<Set<string>>(new Set());

  // Search & filter
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");

  // Rename
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const renameInputRef = useRef<HTMLInputElement>(null);

  // Confirm dialogs
  const [deleteTarget, setDeleteTarget] = useState<AgentInfo | null>(null);

  // Toasts
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const toastIdRef = useRef(0);

  const showToast = useCallback((message: string, type: "success" | "error") => {
    const id = ++toastIdRef.current;
    setToasts((prev) => [...prev, { id, message, type }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 3000);
  }, []);

  const dismissToast = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  // --- Data loading ---

  const loadAgents = useCallback(async (showLoading = false) => {
    if (showLoading) setLoading(true);
    try {
      const res = await fetch("/api/agents");
      const data = await res.json();
      setAgents(data);
    } catch (e) {
      console.error(e);
    } finally {
      if (showLoading) setLoading(false);
    }
  }, []);

  // 30s auto-refresh
  useEffect(() => {
    const interval = setInterval(() => loadAgents(false), 30000);
    return () => clearInterval(interval);
  }, [loadAgents]);

  // --- Stats ---

  const stats = useMemo(() => {
    const total = agents.length;
    const online = agents.filter((a) => a.online).length;
    const offline = agents.filter((a) => !a.online && a.paired).length;
    const unpaired = agents.filter((a) => !a.paired).length;
    return { total, online, offline, unpaired };
  }, [agents]);

  // --- Filtered nodes ---

  const filteredAgents = useMemo(() => {
    let result = agents;

    if (statusFilter === "online") result = result.filter((a) => a.online);
    else if (statusFilter === "offline") result = result.filter((a) => !a.online && a.paired);
    else if (statusFilter === "unpaired") result = result.filter((a) => !a.paired);

    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter(
        (a) =>
          a.name?.toLowerCase().includes(q) ||
          a.hostname?.toLowerCase().includes(q) ||
          a.osInfo?.toLowerCase().includes(q) ||
          a.tokenName?.toLowerCase().includes(q) ||
          a.id.toLowerCase().includes(q)
      );
    }

    return result;
  }, [agents, statusFilter, searchQuery]);

  // --- Actions ---

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      const res = await fetch(`/api/agents/${deleteTarget.id}`, { method: "DELETE" });
      if (!res.ok) throw new Error();
      showToast("节点已删除", "success");
      loadAgents(false);
    } catch {
      showToast("删除失败，请重试", "error");
    }
    setDeleteTarget(null);
  };

  const handleRename = async (agentId: string) => {
    const trimmed = renameValue.trim();
    if (!trimmed) {
      setRenamingId(null);
      return;
    }
    try {
      const res = await fetch(`/api/agents/${agentId}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: trimmed }),
      });
      if (!res.ok) throw new Error();
      showToast("名称已更新", "success");
      loadAgents(false);
    } catch {
      showToast("重命名失败，请重试", "error");
    }
    setRenamingId(null);
  };

  const startRename = (agent: AgentInfo) => {
    setRenamingId(agent.id);
    setRenameValue(agent.name || agent.hostname || "");
    setTimeout(() => renameInputRef.current?.focus(), 0);
  };

  const handleCopy = (text: string, key: string) => {
    navigator.clipboard.writeText(text);
    setCopied(key);
    setTimeout(() => setCopied(null), 2000);
  };

  const toggleCmd = (agentId: string) => {
    setShowCmdFor((prev) => {
      const next = new Set(prev);
      if (next.has(agentId)) next.delete(agentId);
      else next.add(agentId);
      return next;
    });
  };

  const startCommand = "termx remote enable";

  const openAddDialog = () => {
    if (!hasSubscription) {
      setSubscriptionDialogOpen(true);
      return;
    }
    setDialogOpen(true);
  };

  const getAgentDisplayName = (agent: AgentInfo) =>
    agent.name || agent.hostname || agent.id.slice(0, 8);

  const filterButtons: { label: string; value: StatusFilter }[] = [
    { label: "全部", value: "all" },
    { label: "在线", value: "online" },
    { label: "离线", value: "offline" },
    { label: "未配对", value: "unpaired" },
  ];

  // --- Render ---

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-3xl font-bold tracking-tight">我的节点</h2>
          <p className="text-zinc-400">管理 TermX 节点，查看连接状态。</p>
        </div>
        <Button
          onClick={openAddDialog}
          className="bg-primary text-black hover:bg-primary/90"
        >
          <Plus className="w-4 h-4 mr-2" />
          添加节点
        </Button>
      </div>

      {/* Stats Cards */}
      <div className="grid gap-4 grid-cols-2 lg:grid-cols-4">
        <StatsCard
          title="节点总数"
          value={String(stats.total)}
          icon={<ServerIcon className="w-4 h-4 text-zinc-500" />}
          description="已注册节点"
        />
        <StatsCard
          title="在线"
          value={String(stats.online)}
          icon={<Monitor className="w-4 h-4 text-green-500" />}
          description="当前连接中"
        />
        <StatsCard
          title="离线"
          value={String(stats.offline)}
          icon={<Clock className="w-4 h-4 text-zinc-500" />}
          description="已配对但断开"
        />
        <StatsCard
          title="未配对"
          value={String(stats.unpaired)}
          icon={<QrCode className="w-4 h-4 text-amber-400" />}
          description="等待扫码配对"
        />
      </div>

      {/* Search + Filter */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center gap-3">
        <div className="relative flex-1 w-full sm:max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-zinc-500" />
          <Input
            placeholder="搜索节点名称、主机名..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-9 bg-black border-zinc-800 text-white placeholder:text-zinc-600"
          />
        </div>
        <div className="flex items-center gap-1">
          {filterButtons.map((fb) => (
            <Button
              key={fb.value}
              variant={statusFilter === fb.value ? "default" : "ghost"}
              size="sm"
              onClick={() => setStatusFilter(fb.value)}
              className={
                statusFilter === fb.value
                  ? "bg-primary text-black hover:bg-primary/90"
                  : "text-zinc-400 hover:text-white"
              }
            >
              {fb.label}
            </Button>
          ))}
        </div>
      </div>

      {/* Loading State */}
      {loading ? (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="w-8 h-8 text-zinc-500 animate-spin" />
        </div>
      ) : agents.length === 0 ? (
        /* Empty state: no nodes at all */
        <Card className="bg-zinc-900 border-zinc-800">
          <CardContent className="py-12">
            <div className="text-center text-zinc-500">
              <ServerIcon className="w-12 h-12 mx-auto mb-4 opacity-20" />
              <p className="mb-2 text-lg">暂无节点</p>
              <p className="text-sm mb-4">点击下方按钮查看如何在目标机器上启动本机服务</p>
              <Button
                onClick={openAddDialog}
                className="bg-primary text-black hover:bg-primary/90"
              >
                <Plus className="w-4 h-4 mr-2" />
                添加节点
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : filteredAgents.length === 0 ? (
        /* Empty state: no matching results */
        <Card className="bg-zinc-900 border-zinc-800">
          <CardContent className="py-12">
            <div className="text-center text-zinc-500">
              <Search className="w-12 h-12 mx-auto mb-4 opacity-20" />
              <p className="mb-2">没有匹配的节点</p>
              <p className="text-sm mb-4">尝试调整搜索条件或筛选项</p>
              <Button
                variant="ghost"
                onClick={() => {
                  setSearchQuery("");
                  setStatusFilter("all");
                }}
                className="text-zinc-400 hover:text-white"
              >
                清除筛选
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : (
        /* Node cards */
        <div className="space-y-3">
          {filteredAgents.map((agent) => {
            const cmdVisible = showCmdFor.has(agent.id);
            const isRenaming = renamingId === agent.id;

            return (
              <Card key={agent.id} className="bg-zinc-900 border-zinc-800 overflow-hidden">
                <CardContent className="p-0">
                  {/* Row 1: Status + Name + Badge + Actions */}
                  <div className="flex items-center justify-between p-4 pb-1">
                    <div className="flex items-center gap-3 min-w-0">
                      <div
                        className={`w-2.5 h-2.5 rounded-full shrink-0 ${
                          !agent.paired
                            ? "bg-amber-400 shadow-[0_0_8px_rgba(251,191,36,0.5)]"
                            : agent.online
                            ? "bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.5)]"
                            : "bg-zinc-600"
                        }`}
                      />
                      <ServerIcon className="w-4 h-4 text-zinc-500 shrink-0" />

                      {isRenaming ? (
                        <Input
                          ref={renameInputRef}
                          value={renameValue}
                          onChange={(e) => setRenameValue(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") handleRename(agent.id);
                            if (e.key === "Escape") setRenamingId(null);
                          }}
                          onBlur={() => handleRename(agent.id)}
                          className="h-7 w-48 bg-black border-zinc-700 text-white text-sm px-2"
                        />
                      ) : (
                        <div className="flex items-center gap-1.5 min-w-0">
                          <span className="font-medium text-white truncate">
                            {getAgentDisplayName(agent)}
                          </span>
                          <button
                            onClick={() => startRename(agent)}
                            className="text-zinc-600 hover:text-zinc-300 shrink-0"
                            title="重命名"
                          >
                            <Pencil className="w-3 h-3" />
                          </button>
                        </div>
                      )}

                      {!agent.paired ? (
                        <Badge
                          variant="outline"
                          className="text-xs border-amber-500/50 text-amber-400 shrink-0"
                        >
                          未配对
                        </Badge>
                      ) : (
                        <Badge
                          variant={agent.online ? "default" : "secondary"}
                          className={`text-xs shrink-0 ${
                            agent.online
                              ? "bg-green-500/10 text-green-400 border-green-500/30"
                              : ""
                          }`}
                        >
                          {agent.online ? "在线" : "离线"}
                        </Badge>
                      )}
                    </div>

                    <div className="flex items-center gap-1 shrink-0">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-zinc-400 hover:text-primary h-8 w-8"
                        title="启动命令"
                        onClick={() => toggleCmd(agent.id)}
                      >
                        <Terminal className="w-4 h-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-zinc-400 hover:text-red-500 h-8 w-8"
                        title="删除"
                        onClick={() => setDeleteTarget(agent)}
                      >
                        <Trash2 className="w-4 h-4" />
                      </Button>
                    </div>
                  </div>

                  {/* Row 2: Meta info */}
                  <div className="flex items-center gap-2 px-4 pb-1 text-xs text-zinc-500 flex-wrap">
                    {agent.osInfo && (
                      <>
                        <span>{agent.osInfo}</span>
                        <span>·</span>
                      </>
                    )}
                    {agent.tokenName && (
                      <>
                        <span className="flex items-center gap-1">
                          <Key className="w-3 h-3" />
                          {agent.tokenName}
                        </span>
                        <span>·</span>
                      </>
                    )}
                    {agent.hubHttpUrl && (
                      <span className="flex items-center gap-1">
                        <Globe className="w-3 h-3" />
                        {agent.hubHttpUrl.replace(/^https?:\/\//, "")}
                      </span>
                    )}
                  </div>

                  {/* Row 3: Timestamps */}
                  <div className="flex items-center gap-2 px-4 pb-4 text-xs text-zinc-600 flex-wrap">
                    {agent.lastSeen && (
                      <>
                        <span>最后在线: {formatRelativeTime(agent.lastSeen)}</span>
                        <span>·</span>
                      </>
                    )}
                    <span>注册于: {formatRelativeTime(agent.createdAt)}</span>
                  </div>

                  {/* Collapsible command area */}
                  {cmdVisible && (
                    <div className="border-t border-zinc-800 bg-zinc-950/50 p-4">
                      <label className="text-xs text-zinc-500 mb-2 block">启动命令</label>
                      <div className="flex items-start gap-2">
                        <code className="flex-1 text-xs text-green-300 bg-black/50 p-3 rounded border border-green-800/50 break-all select-all leading-relaxed">
                          termx remote enable
                        </code>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => handleCopy(startCommand, `cmd-${agent.id}`)}
                          className="text-green-400 shrink-0 mt-1"
                        >
                          {copied === `cmd-${agent.id}` ? (
                            <Check className="w-4 h-4" />
                          ) : (
                            <Copy className="w-4 h-4" />
                          )}
                        </Button>
                      </div>
                    </div>
                  )}
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}

      {/* Add Node Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>添加节点</DialogTitle>
            <DialogDescription>
              在目标机器上登录本机服务，然后用 Android 客户端扫描二维码。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            {/* Step 1 */}
            <div className="flex gap-3">
              <div className="flex flex-col items-center">
                <div className="w-7 h-7 rounded-full bg-primary/10 border border-primary/30 flex items-center justify-center text-xs font-bold text-primary shrink-0">
                  1
                </div>
                <div className="flex-1 w-px bg-zinc-800 mt-1" />
              </div>
              <div className="space-y-2 pb-2 flex-1">
                <label className="text-sm text-zinc-300 font-medium">浏览器登录</label>
                <div className="p-3 rounded border-l-2 border-primary/50 bg-zinc-950/50">
                  <p className="text-sm text-zinc-400">
                    运行启动命令后，TermX 会打开浏览器授权页；自动化场景可以到“连接密钥”页面创建密钥。
                  </p>
                </div>
              </div>
            </div>

            {/* Step 2 */}
            <div className="flex gap-3">
              <div className="flex flex-col items-center">
                <div className="w-7 h-7 rounded-full bg-primary/10 border border-primary/30 flex items-center justify-center text-xs font-bold text-primary shrink-0">
                  2
                </div>
                <div className="flex-1 w-px bg-zinc-800 mt-1" />
              </div>
              <div className="space-y-2 pb-2 flex-1">
                <label className="text-sm text-zinc-300 font-medium">在目标机器上运行</label>
                <div className="flex items-start gap-2">
                  <code className="flex-1 text-xs text-green-300 bg-black/50 p-3 rounded border border-green-800/50 break-all select-all leading-relaxed">
                    {startCommand}
                  </code>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => handleCopy(startCommand, "cmd-new")}
                    className="text-green-400 shrink-0 mt-1"
                  >
                    {copied === "cmd-new" ? (
                      <Check className="w-4 h-4" />
                    ) : (
                      <Copy className="w-4 h-4" />
                    )}
                  </Button>
                </div>
              </div>
            </div>

            {/* Step 3 */}
            <div className="flex gap-3">
              <div className="flex flex-col items-center">
                <div className="w-7 h-7 rounded-full bg-primary/10 border border-primary/30 flex items-center justify-center text-xs font-bold text-primary shrink-0">
                  3
                </div>
              </div>
              <div className="space-y-2 flex-1">
                <label className="text-sm text-zinc-300 font-medium">扫描二维码</label>
                <div className="flex items-center gap-3 p-3 rounded border-l-2 border-primary/50 bg-zinc-950/50">
                  <QrCode className="w-8 h-8 text-zinc-500" />
                  <div className="text-sm text-zinc-400">
                    <p>本机服务启动后会显示一个二维码</p>
                    <p className="text-xs text-zinc-500 mt-1">
                      用 Android 客户端扫描二维码，完成配对并保存远程控制密钥
                    </p>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => setDialogOpen(false)}
              className="text-zinc-400"
            >
              关闭
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
              添加远程节点需要有效订阅。升级后即可通过 Hub 远程访问和管理服务器。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => setSubscriptionDialogOpen(false)}
              className="text-zinc-400"
            >
              取消
            </Button>
            <Link href="/dashboard/billing">
              <Button className="bg-primary text-black">查看订阅计划</Button>
            </Link>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Confirm Dialog */}
      <Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
            <DialogDescription>
              确定要删除节点{" "}
              <span className="font-medium text-white">
                {deleteTarget ? getAgentDisplayName(deleteTarget) : ""}
              </span>
              ？删除后需要重新注册。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => setDeleteTarget(null)}
              className="text-zinc-400"
            >
              取消
            </Button>
            <Button
              onClick={handleDelete}
              className="bg-red-600 text-white hover:bg-red-700"
            >
              <Trash2 className="w-4 h-4 mr-2" />
              删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Toast Notifications */}
      <ToastContainer toasts={toasts} onDismiss={dismissToast} />
    </div>
  );
}
