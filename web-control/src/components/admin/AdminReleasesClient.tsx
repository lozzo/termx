"use client";

import { useState, useCallback } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Terminal, ChevronDown, ChevronUp } from "lucide-react";

interface AgentRelease {
  id: number;
  version: string;
  os: string;
  arch: string;
  downloadUrl: string;
  sha256: string;
  changelog: string;
  forceUpdate: boolean;
  active: boolean;
  minAppVersion: string;
  createdAt: string;
}

interface AppRelease {
  id: number;
  platform: string;
  type: string;
  version: string;
  versionCode: number;
  fileName: string;
  fileSize: number;
  fileHash: string;
  changelog: string;
  forceUpdate: boolean;
  minAgentVersion: string;
  createdAt: string;
}

interface AdminReleasesClientProps {
  initialAgentReleases: AgentRelease[];
  initialAppReleases: AppRelease[];
}

export default function AdminReleasesClient({ initialAgentReleases, initialAppReleases }: AdminReleasesClientProps) {
  const [tab, setTab] = useState<"agent" | "app">("agent");
  const [agentReleases, setAgentReleases] = useState<AgentRelease[]>(initialAgentReleases);
  const [appReleases, setAppReleases] = useState<AppRelease[]>(initialAppReleases);
  const [showGuide, setShowGuide] = useState(false);

  const fetchAgentReleases = useCallback(async () => {
    try {
      const res = await fetch("/api/admin/releases/agent");
      if (res.ok) setAgentReleases(await res.json());
    } catch { /* */ }
  }, []);

  const fetchAppReleases = useCallback(async () => {
    try {
      const res = await fetch("/api/admin/releases/app");
      if (res.ok) setAppReleases(await res.json());
    } catch { /* */ }
  }, []);

  const handleToggleAgentActive = async (r: AgentRelease) => {
    await fetch(`/api/admin/releases/agent/${r.id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ active: !r.active }),
    });
    fetchAgentReleases();
  };

  const handleToggleAgentForce = async (r: AgentRelease) => {
    await fetch(`/api/admin/releases/agent/${r.id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ forceUpdate: !r.forceUpdate }),
    });
    fetchAgentReleases();
  };

  const handleToggleAppForce = async (r: AppRelease) => {
    await fetch(`/api/admin/releases/app/${r.id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ forceUpdate: !r.forceUpdate }),
    });
    fetchAppReleases();
  };

  const formatSize = (bytes: number) => {
    if (bytes === 0) return "-";
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-3xl font-bold tracking-tight">版本管理</h2>
        <p className="text-zinc-400">查看和管理 termx 本机服务与客户端的发布版本。</p>
      </div>

      {/* 发布指南 */}
      <Card className="bg-zinc-900 border-zinc-800">
        <button
          onClick={() => setShowGuide(!showGuide)}
          className="w-full flex items-center justify-between px-6 py-4 text-left"
        >
          <div className="flex items-center gap-2">
            <Terminal className="w-4 h-4 text-zinc-400" />
            <span className="text-sm font-medium text-zinc-300">如何发布新版本？</span>
          </div>
          {showGuide
            ? <ChevronUp className="w-4 h-4 text-zinc-400" />
            : <ChevronDown className="w-4 h-4 text-zinc-400" />
          }
        </button>
        {showGuide && (
          <CardContent className="pt-0 pb-5">
            <div className="text-sm text-zinc-400 space-y-4">
              <p className="text-zinc-300">
                版本发布通过 <code className="bg-zinc-800 px-1.5 py-0.5 rounded text-xs text-emerald-400">termx-app</code> 项目的 Makefile 执行，需要先配置 S3 环境变量。
              </p>

              <div>
                <p className="text-zinc-300 font-medium mb-1">环境变量（必须）</p>
                <pre className="bg-zinc-950 border border-zinc-800 rounded-md p-3 text-xs overflow-x-auto">{`export S3_ENDPOINT=https://s3-la.omscd.com
export S3_ACCESS_KEY=<your-access-key>
export S3_SECRET_KEY=<your-secret-key>
export S3_BUCKET=<your-bucket>`}</pre>
              </div>

              <div>
                <p className="text-zinc-300 font-medium mb-1">发布本机服务（守护进程二进制）</p>
                <pre className="bg-zinc-950 border border-zinc-800 rounded-md p-3 text-xs overflow-x-auto">{`# 构建 darwin/amd64, darwin/arm64, freebsd/amd64, freebsd/arm64,
# linux/386, linux/amd64, linux/arm, linux/arm64, linux/riscv64 并上传
make release-agent VERSION=1.0.3 ADMIN_USER=admin ADMIN_PASS=xxx \\
  CHANGELOG="修复了连接问题"`}</pre>
              </div>

              <div>
                <p className="text-zinc-300 font-medium mb-1">发布 APK（Android 整包安装）</p>
                <pre className="bg-zinc-950 border border-zinc-800 rounded-md p-3 text-xs overflow-x-auto">{`make release-apk VERSION=1.1.0 VERSION_CODE=2 \\
  ADMIN_USER=admin ADMIN_PASS=xxx`}</pre>
              </div>

              <div>
                <p className="text-zinc-300 font-medium mb-1">一键发布全部</p>
                <pre className="bg-zinc-950 border border-zinc-800 rounded-md p-3 text-xs overflow-x-auto">{`make release-all VERSION=1.0.3 VERSION_CODE=3 \\
  ADMIN_USER=admin ADMIN_PASS=xxx CHANGELOG="新功能"`}</pre>
              </div>

              <div>
                <p className="text-zinc-300 font-medium mb-1">部署 Hub（中继服务）</p>
                <pre className="bg-zinc-950 border border-zinc-800 rounded-md p-3 text-xs overflow-x-auto">{`make deploy-hub VERSION=1.0.3`}</pre>
              </div>

              <p className="text-zinc-500 text-xs">
                脚本会自动完成：构建 → 上传 S3 → 注册到数据库。发布成功后刷新本页面即可看到新版本。
              </p>
            </div>
          </CardContent>
        )}
      </Card>

      {/* Tab 切换 */}
      <div className="flex gap-2">
        <Button
          variant={tab === "agent" ? "default" : "outline"}
          size="sm"
          onClick={() => setTab("agent")}
        >
          本机服务版本
        </Button>
        <Button
          variant={tab === "app" ? "default" : "outline"}
          size="sm"
          onClick={() => setTab("app")}
        >
          客户端版本
        </Button>
      </div>

      {tab === "agent" ? (
        <Card className="bg-zinc-900 border-zinc-800">
          <CardHeader>
            <CardTitle className="text-base">本机服务版本 ({agentReleases.length})</CardTitle>
          </CardHeader>
          <CardContent>
            {agentReleases.length === 0 ? (
              <p className="text-center text-zinc-500 py-8">暂无版本</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-zinc-800 text-zinc-400">
                      <th className="text-left py-3 px-2 font-medium">版本</th>
                      <th className="text-left py-3 px-2 font-medium">OS/Arch</th>
                      <th className="text-left py-3 px-2 font-medium">SHA256</th>
                      <th className="text-left py-3 px-2 font-medium">最低客户端版本</th>
                      <th className="text-left py-3 px-2 font-medium">更新日志</th>
                      <th className="text-left py-3 px-2 font-medium">启用</th>
                      <th className="text-left py-3 px-2 font-medium">强制更新</th>
                      <th className="text-left py-3 px-2 font-medium">时间</th>
                    </tr>
                  </thead>
                  <tbody>
                    {agentReleases.map((r) => (
                      <tr key={r.id} className="border-b border-zinc-800/50">
                        <td className="py-3 px-2 font-mono font-semibold text-white">{r.version}</td>
                        <td className="py-3 px-2 text-zinc-400 text-xs">{r.os}/{r.arch}</td>
                        <td className="py-3 px-2 text-zinc-400 text-xs font-mono max-w-[120px] truncate" title={r.sha256}>{r.sha256 ? r.sha256.slice(0, 12) + "..." : "-"}</td>
                        <td className="py-3 px-2 text-zinc-400 text-xs font-mono">{r.minAppVersion || "-"}</td>
                        <td className="py-3 px-2 text-zinc-400 text-xs max-w-[150px] truncate" title={r.changelog}>{r.changelog || "-"}</td>
                        <td className="py-3 px-2">
                          <Switch checked={r.active} onCheckedChange={() => handleToggleAgentActive(r)} />
                        </td>
                        <td className="py-3 px-2">
                          <Switch checked={r.forceUpdate} onCheckedChange={() => handleToggleAgentForce(r)} />
                        </td>
                        <td className="py-3 px-2 text-zinc-400 text-xs">
                          {new Date(r.createdAt).toLocaleDateString()}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>
      ) : (
        <Card className="bg-zinc-900 border-zinc-800">
          <CardHeader>
            <CardTitle className="text-base">客户端版本 ({appReleases.length})</CardTitle>
          </CardHeader>
          <CardContent>
            {appReleases.length === 0 ? (
              <p className="text-center text-zinc-500 py-8">暂无版本</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-zinc-800 text-zinc-400">
                      <th className="text-left py-3 px-2 font-medium">版本</th>
                      <th className="text-left py-3 px-2 font-medium">平台</th>
                      <th className="text-left py-3 px-2 font-medium">版本号</th>
                      <th className="text-left py-3 px-2 font-medium">文件名</th>
                      <th className="text-left py-3 px-2 font-medium">大小</th>
                      <th className="text-left py-3 px-2 font-medium">最低本机服务版本</th>
                      <th className="text-left py-3 px-2 font-medium">强制更新</th>
                      <th className="text-left py-3 px-2 font-medium">时间</th>
                    </tr>
                  </thead>
                  <tbody>
                    {appReleases.map((r) => (
                      <tr key={r.id} className="border-b border-zinc-800/50">
                        <td className="py-3 px-2 font-mono font-semibold text-white">{r.version}</td>
                        <td className="py-3 px-2 text-zinc-400">{r.platform}</td>
                        <td className="py-3 px-2 text-zinc-300">{r.versionCode}</td>
                        <td className="py-3 px-2 text-zinc-400 text-xs font-mono max-w-[200px] truncate">{r.fileName}</td>
                        <td className="py-3 px-2 text-zinc-400 text-xs">{formatSize(r.fileSize)}</td>
                        <td className="py-3 px-2 text-zinc-400 text-xs font-mono">{r.minAgentVersion || "-"}</td>
                        <td className="py-3 px-2">
                          <Switch checked={r.forceUpdate} onCheckedChange={() => handleToggleAppForce(r)} />
                        </td>
                        <td className="py-3 px-2 text-zinc-400 text-xs">
                          {new Date(r.createdAt).toLocaleDateString()}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
