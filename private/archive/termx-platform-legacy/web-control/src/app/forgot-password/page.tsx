"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Terminal, Loader2, ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  CardFooter,
} from "@/components/ui/card";

export default function ForgotPasswordPage() {
  const router = useRouter();
  const [step, setStep] = useState<1 | 2>(1);
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [countdown, setCountdown] = useState(0);

  function startCountdown() {
    setCountdown(60);
    const timer = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          clearInterval(timer);
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
  }

  async function handleSendCode(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setIsLoading(true);

    try {
      const res = await fetch("/api/auth/forgot-password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });

      const data = await res.json();

      if (!res.ok) {
        setError(data.error);
        return;
      }

      setStep(2);
      setSuccess("验证码已发送到您的邮箱");
      startCountdown();
    } catch {
      setError("发送失败，请稍后重试");
    } finally {
      setIsLoading(false);
    }
  }

  async function handleResendCode() {
    if (countdown > 0) return;
    setError("");
    setIsLoading(true);

    try {
      const res = await fetch("/api/auth/forgot-password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });

      const data = await res.json();

      if (!res.ok) {
        setError(data.error);
        return;
      }

      setSuccess("验证码已重新发送");
      startCountdown();
    } catch {
      setError("发送失败，请稍后重试");
    } finally {
      setIsLoading(false);
    }
  }

  async function handleResetPassword(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setSuccess("");
    setIsLoading(true);

    try {
      const res = await fetch("/api/auth/reset-password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, code, newPassword }),
      });

      const data = await res.json();

      if (!res.ok) {
        setError(data.error);
        return;
      }

      router.push("/login");
    } catch {
      setError("重置失败，请稍后重试");
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
        <Link
          href="/"
          className="flex items-center justify-center gap-2 font-mono font-bold text-2xl tracking-tighter mb-8"
        >
          <Terminal className="w-8 h-8 text-primary" />
          <span>termx</span>
        </Link>

        <Card className="bg-zinc-900/50 backdrop-blur-sm border-zinc-800">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">找回密码</CardTitle>
            <CardDescription>
              {step === 1
                ? "输入您的注册邮箱，我们将发送验证码"
                : "输入验证码和新密码"}
            </CardDescription>
          </CardHeader>

          {step === 1 ? (
            <form onSubmit={handleSendCode}>
              <CardContent className="space-y-4">
                {error && (
                  <div className="p-3 rounded-md bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
                    {error}
                  </div>
                )}
                <div className="grid gap-2">
                  <Label htmlFor="email" className="text-zinc-300">
                    邮箱
                  </Label>
                  <Input
                    id="email"
                    type="email"
                    placeholder="请输入注册邮箱"
                    className="bg-black border-zinc-800 text-white"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
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
                      发送中...
                    </>
                  ) : (
                    "发送验证码"
                  )}
                </Button>
                <Link
                  href="/login"
                  className="text-sm text-zinc-500 hover:text-zinc-300 flex items-center gap-1"
                >
                  <ArrowLeft className="w-3 h-3" />
                  返回登录
                </Link>
              </CardFooter>
            </form>
          ) : (
            <form onSubmit={handleResetPassword}>
              <CardContent className="space-y-4">
                {error && (
                  <div className="p-3 rounded-md bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
                    {error}
                  </div>
                )}
                {success && (
                  <div className="p-3 rounded-md bg-green-500/10 border border-green-500/20 text-green-400 text-sm">
                    {success}
                  </div>
                )}
                <div className="grid gap-2">
                  <Label htmlFor="code" className="text-zinc-300">
                    验证码
                  </Label>
                  <Input
                    id="code"
                    placeholder="请输入6位验证码"
                    className="bg-black border-zinc-800 text-white text-center text-lg tracking-widest"
                    value={code}
                    onChange={(e) =>
                      setCode(e.target.value.replace(/\D/g, "").slice(0, 6))
                    }
                    required
                    disabled={isLoading}
                    maxLength={6}
                  />
                  <button
                    type="button"
                    onClick={handleResendCode}
                    disabled={countdown > 0 || isLoading}
                    className="text-xs text-zinc-500 hover:text-primary disabled:hover:text-zinc-500 text-right"
                  >
                    {countdown > 0
                      ? `${countdown}秒后可重新发送`
                      : "重新发送验证码"}
                  </button>
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="newPassword" className="text-zinc-300">
                    新密码
                  </Label>
                  <Input
                    id="newPassword"
                    type="password"
                    placeholder="请输入新密码（至少6位）"
                    className="bg-black border-zinc-800 text-white"
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    required
                    disabled={isLoading}
                    minLength={6}
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
                      重置中...
                    </>
                  ) : (
                    "重置密码"
                  )}
                </Button>
                <button
                  type="button"
                  onClick={() => {
                    setStep(1);
                    setError("");
                    setSuccess("");
                  }}
                  className="text-sm text-zinc-500 hover:text-zinc-300 flex items-center gap-1"
                >
                  <ArrowLeft className="w-3 h-3" />
                  返回上一步
                </button>
              </CardFooter>
            </form>
          )}
        </Card>
      </div>
    </div>
  );
}
