"use client"

import { Suspense, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Terminal, Loader2, Github } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle, CardDescription, CardFooter } from "@/components/ui/card";
import { useAuth } from "@/components/AuthProvider";
import { getGithubAuthErrorMessage } from "@/lib/github-auth-error";

export default function LoginPage() {
  return (
    <Suspense>
      <LoginForm />
    </Suspense>
  );
}

function LoginForm() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const { login } = useAuth();
  const searchParams = useSearchParams();
  const from = searchParams.get("from");
  const oauthError = searchParams.get("error");
  const githubLoginHref = `/api/auth/github?entry=login${from ? `&from=${encodeURIComponent(from)}` : ""}`;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setIsLoading(true);

    try {
      await login(username, password, from || undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败");
    } finally {
      setIsLoading(false);
    }
  }

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

        <Card className="bg-zinc-900/50 backdrop-blur-sm border-zinc-800">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">登录</CardTitle>
            <CardDescription>输入您的账户信息以登录控制台</CardDescription>
          </CardHeader>
          <form onSubmit={onSubmit}>
            <CardContent className="space-y-4">
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
              {from && (
                <div className="p-3 rounded-md bg-yellow-500/10 border border-yellow-500/20 text-yellow-400 text-sm">
                  请先登录后再访问该页面
                </div>
              )}
              <div className="grid gap-2">
                <Label htmlFor="username" className="text-zinc-300">用户名</Label>
                <Input
                  id="username"
                  placeholder="请输入用户名"
                  className="bg-black border-zinc-800 text-white"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  required
                  disabled={isLoading}
                />
              </div>
              <div className="grid gap-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="password" className="text-zinc-300">密码</Label>
                  <Link href="/forgot-password" className="text-xs text-zinc-500 hover:text-primary">
                    忘记密码？
                  </Link>
                </div>
                <Input
                  id="password"
                  type="password"
                  placeholder="请输入密码"
                  className="bg-black border-zinc-800 text-white"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
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
                    登录中...
                  </>
                ) : (
                  "登录"
                )}
              </Button>
              <Button
                type="button"
                variant="outline"
                className="w-full border-zinc-700 bg-zinc-950 text-white hover:bg-zinc-900"
                disabled={isLoading}
                asChild
              >
                <a href={githubLoginHref} className="flex items-center justify-center gap-2">
                  <Github className="w-4 h-4" />
                  <span>使用 GitHub 登录</span>
                </a>
              </Button>
              <p className="text-sm text-zinc-500 text-center">
                还没有账户？{" "}
                <Link href={`/register${from ? `?from=${encodeURIComponent(from)}` : ""}`} className="text-primary hover:underline">
                  注册
                </Link>
              </p>
            </CardFooter>
          </form>
        </Card>
      </div>
    </div>
  );
}
