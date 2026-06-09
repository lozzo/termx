"use client"

import { useState, Suspense } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Terminal, Loader2, Github } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle, CardDescription, CardFooter } from "@/components/ui/card";
import { useAuth } from "@/components/AuthProvider";
import { getGithubAuthErrorMessage } from "@/lib/github-auth-error";

function RegisterForm() {
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const { register } = useAuth();
  const searchParams = useSearchParams();
  const referralCode = searchParams.get("ref") || undefined;
  const from = searchParams.get("from");
  const oauthError = searchParams.get("error");
  const githubRegisterHref = `/api/auth/github?entry=register${from ? `&from=${encodeURIComponent(from)}` : ""}${referralCode ? `&ref=${encodeURIComponent(referralCode)}` : ""}`;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");

    if (password !== confirmPassword) {
      setError("两次输入的密码不一致");
      return;
    }

    if (password.length < 6) {
      setError("密码长度至少 6 个字符");
      return;
    }

    setIsLoading(true);

    try {
      await register(username, email, password, referralCode, from || undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : "注册失败");
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <Card className="bg-zinc-900/50 backdrop-blur-sm border-zinc-800">
      <CardHeader className="text-center">
        <CardTitle className="text-2xl">注册</CardTitle>
        <CardDescription>创建您的 termx 账户</CardDescription>
      </CardHeader>
      <form onSubmit={onSubmit}>
        <CardContent className="space-y-4">
          {referralCode && (
            <div className="p-3 rounded-md bg-primary/10 border border-primary/20 text-primary text-sm">
              您正通过好友邀请注册，注册后好友将获得奖励
            </div>
          )}
          {error && (
            <div className="p-3 rounded-md bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
              {error}
            </div>
          )}
          {oauthError && (
            <div className="p-3 rounded-md bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
              {getGithubAuthErrorMessage(oauthError)}
            </div>
          )}
          <div className="grid gap-2">
            <Label htmlFor="username" className="text-zinc-300">用户名</Label>
            <Input
              id="username"
              placeholder="请输入用户名 (3-32 个字符)"
              className="bg-black border-zinc-800 text-white"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
              disabled={isLoading}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="email" className="text-zinc-300">邮箱</Label>
            <Input
              id="email"
              type="email"
              placeholder="请输入邮箱"
              className="bg-black border-zinc-800 text-white"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              disabled={isLoading}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="password" className="text-zinc-300">密码</Label>
            <Input
              id="password"
              type="password"
              placeholder="请输入密码 (至少 6 个字符)"
              className="bg-black border-zinc-800 text-white"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              disabled={isLoading}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="confirm-password" className="text-zinc-300">确认密码</Label>
            <Input
              id="confirm-password"
              type="password"
              placeholder="请再次输入密码"
              className="bg-black border-zinc-800 text-white"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              required
              disabled={isLoading}
            />
          </div>
        </CardContent>
        <CardFooter className="flex flex-col gap-4">
          <Button
            type="submit"
            className="w-full bg-primary text-black hover:bg-primary/90 font-bold"
            disabled={isLoading}
          >
            {isLoading ? (
              <>
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                注册中...
              </>
            ) : (
              "注册"
            )}
          </Button>
          <Button
            type="button"
            variant="outline"
            className="w-full border-zinc-700 bg-zinc-950 text-white hover:bg-zinc-900"
            disabled={isLoading}
            asChild
          >
            <a href={githubRegisterHref} className="flex items-center justify-center gap-2">
              <Github className="w-4 h-4" />
              <span>使用 GitHub 注册 / 登录</span>
            </a>
          </Button>
          <p className="text-sm text-zinc-500 text-center">
            已有账户？{" "}
            <Link href={`/login${from ? `?from=${encodeURIComponent(from)}` : ""}`} className="text-primary hover:underline">
              登录
            </Link>
          </p>
        </CardFooter>
      </form>
    </Card>
  );
}

export default function RegisterPage() {
  return (
    <div className="min-h-screen bg-black text-white font-sans flex items-center justify-center relative">
      <div className="fixed inset-0 z-0 pointer-events-none">
        <div className="absolute inset-0 bg-[linear-gradient(to_right,#80808012_1px,transparent_1px),linear-gradient(to_bottom,#80808012_1px,transparent_1px)] bg-[size:24px_24px]"></div>
        <div className="absolute left-0 right-0 top-0 -z-10 m-auto h-[310px] w-[310px] rounded-full bg-primary/20 opacity-20 blur-[100px]"></div>
      </div>

      <div className="relative z-10 w-full max-w-md px-4">
        <Link href="/" className="flex items-center justify-center gap-2 font-mono font-bold text-2xl tracking-tighter mb-8">
          <Terminal className="w-8 h-8 text-primary" />
          <span>termx</span>
        </Link>

        <Suspense fallback={
          <Card className="bg-zinc-900/50 backdrop-blur-sm border-zinc-800">
            <CardContent className="py-8 text-center text-zinc-500">
              <Loader2 className="w-6 h-6 animate-spin mx-auto" />
            </CardContent>
          </Card>
        }>
          <RegisterForm />
        </Suspense>
      </div>
    </div>
  );
}
