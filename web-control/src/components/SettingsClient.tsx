"use client";

import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle, CardDescription, CardFooter } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2 } from "lucide-react";
import { getSafeClientRedirectPath } from "@/lib/url";

interface SettingsClientProps {
  user: {
    id: string;
    username: string;
    email: string;
    role: string;
    githubId: string | null;
    hasLocalPassword: boolean;
  };
  forceSetupLocalPassword?: boolean;
  from?: string | null;
}

export default function SettingsClient({ user, forceSetupLocalPassword = false, from }: SettingsClientProps) {
  const isGithubUser = Boolean(user.githubId);
  const [hasLocalPassword, setHasLocalPassword] = useState(user.hasLocalPassword);
  const [email, setEmail] = useState(user.email);
  const [username, setUsername] = useState(user.username);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [profileMsg, setProfileMsg] = useState("");
  const [pwMsg, setPwMsg] = useState("");
  const [profileLoading, setProfileLoading] = useState(false);
  const [pwLoading, setPwLoading] = useState(false);

  async function saveProfile(e: React.FormEvent) {
    e.preventDefault();
    setProfileMsg("");
    setProfileLoading(true);
    try {
      const res = await fetch("/api/auth/profile", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, username }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error);
      setProfileMsg("保存成功");
    } catch (err) {
      setProfileMsg(err instanceof Error ? err.message : "保存失败");
    } finally {
      setProfileLoading(false);
    }
  }

  async function changePassword(e: React.FormEvent) {
    e.preventDefault();
    setPwMsg("");
    if (hasLocalPassword && !currentPassword) {
      setPwMsg("当前密码和新密码为必填项");
      return;
    }
    if (newPassword.length < 6) {
      setPwMsg("新密码至少 6 个字符");
      return;
    }
    setPwLoading(true);
    try {
      const res = await fetch("/api/auth/password", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ currentPassword, newPassword }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error);
      setCurrentPassword("");
      setNewPassword("");
      const successMsg = hasLocalPassword ? "密码已更新" : "密码已设置";
      setPwMsg(successMsg);
      if (!hasLocalPassword) {
        setHasLocalPassword(true);
      }
      if (!hasLocalPassword && from) {
        window.location.assign(getSafeClientRedirectPath(from));
        return;
      }
    } catch (err) {
      setPwMsg(err instanceof Error ? err.message : "修改失败");
    } finally {
      setPwLoading(false);
    }
  }

  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-3xl font-bold tracking-tight">账户设置</h2>
        <p className="text-zinc-400">管理您的账户偏好设置。</p>
      </div>

      <form onSubmit={saveProfile}>
        <Card className="bg-zinc-900 border-zinc-800">
          <CardHeader>
            <CardTitle>个人资料</CardTitle>
            <CardDescription>更新您的个人信息。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {profileMsg && (
              <div className={`p-3 rounded-md text-sm ${profileMsg === "保存成功" ? "bg-green-500/10 border border-green-500/20 text-green-400" : "bg-red-500/10 border border-red-500/20 text-red-400"}`}>
                {profileMsg}
              </div>
            )}
            {forceSetupLocalPassword && !hasLocalPassword ? (
              <div className="p-3 rounded-md text-sm bg-yellow-500/10 border border-yellow-500/20 text-yellow-300">
                当前 GitHub 账户还没有本地密码。请先设置密码，再使用用户名密码登录 Web 或手机 App。
              </div>
            ) : null}
            <div className="grid gap-2">
              <Label htmlFor="email" className="text-zinc-300">邮箱</Label>
              <Input
                id="email"
                value={email}
                onChange={e => setEmail(e.target.value)}
                disabled={isGithubUser}
                className="bg-black border-zinc-800 text-white disabled:cursor-not-allowed disabled:opacity-60"
              />
              {isGithubUser ? (
                <p className="text-xs text-zinc-500">GitHub 登录账户的邮箱由 GitHub 提供，暂不支持在这里修改。</p>
              ) : null}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="name" className="text-zinc-300">用户名</Label>
              <Input id="name" value={username} onChange={e => setUsername(e.target.value)} className="bg-black border-zinc-800 text-white" />
            </div>
          </CardContent>
          <CardFooter>
            <Button type="submit" className="bg-white text-black hover:bg-zinc-200" disabled={profileLoading}>
              {profileLoading ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : null}
              保存更改
            </Button>
          </CardFooter>
        </Card>
      </form>

      <form onSubmit={changePassword}>
        <Card className="bg-zinc-900 border-zinc-800">
          <CardHeader>
            <CardTitle>密码</CardTitle>
            <CardDescription>
              {hasLocalPassword ? "修改您的登录密码。" : "为当前账户设置一个本地登录密码。"}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {pwMsg && (
              <div className={`p-3 rounded-md text-sm ${pwMsg === "密码已更新" || pwMsg === "密码已设置" ? "bg-green-500/10 border border-green-500/20 text-green-400" : "bg-red-500/10 border border-red-500/20 text-red-400"}`}>
                {pwMsg}
              </div>
            )}
            {!hasLocalPassword ? (
              <p className="text-sm text-zinc-500">这个账户当前还没有可用的本地密码，因此无需填写当前密码。</p>
            ) : (
              <div className="grid gap-2">
                <Label htmlFor="current-password" className="text-zinc-300">当前密码</Label>
                <Input id="current-password" type="password" value={currentPassword} onChange={e => setCurrentPassword(e.target.value)} className="bg-black border-zinc-800 text-white" />
              </div>
            )}
            <div className="grid gap-2">
              <Label htmlFor="new-password" className="text-zinc-300">新密码</Label>
              <Input id="new-password" type="password" value={newPassword} onChange={e => setNewPassword(e.target.value)} className="bg-black border-zinc-800 text-white" />
            </div>
          </CardContent>
          <CardFooter>
            <Button type="submit" className="bg-zinc-800 text-white hover:bg-zinc-700" disabled={pwLoading}>
              {pwLoading ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : null}
              {!hasLocalPassword ? "设置密码" : "更新密码"}
            </Button>
            {from && !hasLocalPassword ? (
              <p className="ml-4 text-xs text-zinc-500">设置完成后可返回之前请求的页面。</p>
            ) : null}
          </CardFooter>
        </Card>
      </form>
    </div>
  );
}
