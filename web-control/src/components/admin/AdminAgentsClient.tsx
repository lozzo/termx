"use client";

import { useState, useCallback } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";

interface AgentItem {
  id: string;
  name: string;
  userId: string;
  username: string | null;
  hostname: string;
  osInfo: string;
  online: boolean;
  hubId: string | null;
  hubName: string | null;
  paired: boolean;
  pendingKick: boolean;
  sessionBytesIn: number | null;
  sessionBytesOut: number | null;
  sessionStartedAt: string | null;
  lastSeen: string | null;
  createdAt: string;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + " MB";
  return (bytes / (1024 * 1024 * 1024)).toFixed(2) + " GB";
}

function formatDuration(startedAt: string): string {
  const ms = Date.now() - new Date(startedAt).getTime();
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return seconds + "s";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return minutes + "m " + (seconds % 60) + "s";
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return hours + "h " + (minutes % 60) + "m";
  const days = Math.floor(hours / 24);
  return days + "d " + (hours % 24) + "h";
}

export default function AdminAgentsClient({ initialAgents }: { initialAgents: AgentItem[] }) {
  const [agents, setAgents] = useState<AgentItem[]>(initialAgents);

  const fetchAgents = useCallback(async () => {
    try {
      const res = await fetch("/api/admin/agents");
      if (res.ok) setAgents(await res.json());
    } catch { /* */ }
  }, []);

  const handleKick = async (id: string) => {
    if (!confirm("确定踢出该 Agent？")) return;
    const res = await fetch(`/api/admin/agents/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ pendingKick: true }),
    });
    if (!res.ok) {
      const data = await res.json();
      alert(data.error || "操作失败");
      return;
    }
    fetchAgents();
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-3xl font-bold tracking-tight">节点概览</h2>
        <p className="text-zinc-400">查看所有 TermX 节点状态。</p>
      </div>

      <Card className="bg-zinc-900 border-zinc-800">
        <CardHeader>
          <CardTitle className="text-base">
            全部节点 ({agents.length})
            {agents.length > 0 && (
              <span className="text-zinc-500 font-normal ml-2">
                在线 {agents.filter(a => a.online).length}
              </span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {agents.length === 0 ? (
            <p className="text-center text-zinc-500 py-8">暂无节点</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-800 text-zinc-400">
                    <th className="text-left py-3 px-2 font-medium">名称</th>
                    <th className="text-left py-3 px-2 font-medium">用户</th>
                    <th className="text-left py-3 px-2 font-medium">主机名</th>
                    <th className="text-left py-3 px-2 font-medium">OS</th>
                    <th className="text-left py-3 px-2 font-medium">在线</th>
                    <th className="text-left py-3 px-2 font-medium">Hub</th>
                    <th className="text-left py-3 px-2 font-medium">本次流量</th>
                    <th className="text-left py-3 px-2 font-medium">在线时长</th>
                    <th className="text-left py-3 px-2 font-medium">配对</th>
                    <th className="text-left py-3 px-2 font-medium">活跃时间</th>
                    <th className="text-right py-3 px-2 font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {agents.map((a) => (
                    <tr key={a.id} className="border-b border-zinc-800/50">
                      <td className="py-3 px-2 font-medium text-white">{a.name || "-"}</td>
                      <td className="py-3 px-2 text-zinc-400">{a.username || "-"}</td>
                      <td className="py-3 px-2 text-zinc-400 text-xs font-mono">{a.hostname || "-"}</td>
                      <td className="py-3 px-2 text-zinc-400 text-xs">{a.osInfo || "-"}</td>
                      <td className="py-3 px-2">
                        <Badge
                          variant="outline"
                          className={a.online ? "border-green-600 text-green-500" : "border-zinc-600 text-zinc-500"}
                        >
                          {a.online ? "在线" : "离线"}
                        </Badge>
                      </td>
                      <td className="py-3 px-2 text-zinc-400 text-xs">{a.hubName || "-"}</td>
                      <td className="py-3 px-2 text-zinc-400 text-xs">
                        {a.online && (a.sessionBytesIn != null || a.sessionBytesOut != null)
                          ? formatBytes((a.sessionBytesIn || 0) + (a.sessionBytesOut || 0))
                          : "-"}
                      </td>
                      <td className="py-3 px-2 text-zinc-400 text-xs">
                        {a.online && a.sessionStartedAt ? formatDuration(a.sessionStartedAt) : "-"}
                      </td>
                      <td className="py-3 px-2">
                        {a.paired ? (
                          <Badge variant="outline" className="border-primary text-primary">已配对</Badge>
                        ) : (
                          <span className="text-zinc-600">未配对</span>
                        )}
                      </td>
                      <td className="py-3 px-2 text-zinc-400 text-xs">
                        {a.lastSeen ? new Date(a.lastSeen).toLocaleString() : "-"}
                      </td>
                      <td className="py-3 px-2 text-right">
                        {a.pendingKick ? (
                          <Badge variant="outline" className="border-yellow-600 text-yellow-500">待踢出</Badge>
                        ) : (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleKick(a.id)}
                            className="text-xs text-red-400 hover:text-red-300"
                          >
                            踢出
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
