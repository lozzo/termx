"use client";

import { Suspense, useMemo, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { CheckCircle2, Loader2, Terminal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useAuth } from "@/components/AuthProvider";

function normalizeCode(input: string): string {
  const compact = input.trim().toUpperCase().replace(/[^A-Z0-9]/g, "");
  if (compact.length !== 8) return input.trim().toUpperCase();
  return `${compact.slice(0, 4)}-${compact.slice(4)}`;
}

export default function DeviceLoginPage() {
  return (
    <Suspense>
      <DeviceLoginForm />
    </Suspense>
  );
}

function DeviceLoginForm() {
  const searchParams = useSearchParams();
  const initialCode = useMemo(() => normalizeCode(searchParams.get("code") || ""), [searchParams]);
  const [code, setCode] = useState(initialCode);
  const [error, setError] = useState("");
  const [approved, setApproved] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const { user } = useAuth();

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setIsLoading(true);
    try {
      const res = await fetch("/api/v1/auth/browser/approve", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ user_code: normalizeCode(code) }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        throw new Error(data?.error?.message || data?.error || "授权失败");
      }
      setApproved(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "授权失败");
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <div className="min-h-screen bg-black text-white font-sans flex items-center justify-center relative">
      <div className="fixed inset-0 z-0 pointer-events-none">
        <div className="absolute inset-0 bg-[linear-gradient(to_right,#80808012_1px,transparent_1px),linear-gradient(to_bottom,#80808012_1px,transparent_1px)] bg-[size:24px_24px]" />
        <div className="absolute left-0 right-0 top-0 -z-10 m-auto h-[310px] w-[310px] rounded-full bg-primary/20 opacity-20 blur-[100px]" />
      </div>

      <div className="relative z-10 w-full max-w-md px-4">
        <Link href="/" className="flex items-center justify-center gap-2 font-mono font-bold text-2xl mb-8">
          <Terminal className="w-8 h-8 text-primary" />
          <span>termx</span>
        </Link>

        <Card className="bg-zinc-900/50 backdrop-blur-sm border-zinc-800">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">授权本机服务</CardTitle>
            <CardDescription>
              {user ? `当前账户：${user.email}` : "确认后 CLI 会自动完成远程连接配置"}
            </CardDescription>
          </CardHeader>
          <form onSubmit={onSubmit}>
            <CardContent className="space-y-4">
              {approved ? (
                <div className="rounded-md border border-emerald-500/20 bg-emerald-500/10 p-4 text-sm text-emerald-300">
                  <div className="flex items-center gap-2 font-medium">
                    <CheckCircle2 className="h-4 w-4" />
                    授权成功
                  </div>
                  <p className="mt-2 text-emerald-200/80">可以回到终端，TermX 会继续自动完成配置。</p>
                </div>
              ) : (
                <>
                  {error && (
                    <div className="p-3 rounded-md bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
                      {error}
                    </div>
                  )}
                  <div className="grid gap-2">
                    <Label htmlFor="browser" className="text-zinc-300">
                      授权码
                    </Label>
                    <Input
                      id="browser"
                      value={code}
                      onChange={(e) => setCode(e.target.value)}
                      className="bg-black border-zinc-800 text-white text-center font-mono text-lg tracking-[0.2em]"
                      placeholder="ABCD-EFGH"
                      required
                      disabled={isLoading}
                    />
                  </div>
                </>
              )}
            </CardContent>
            <CardFooter className="flex flex-col gap-4">
              {!approved && (
                <Button
                  type="submit"
                  className="w-full bg-primary text-black hover:bg-primary/90 font-bold"
                  disabled={isLoading}
                >
                  {isLoading ? (
                    <>
                      <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                      授权中...
                    </>
                  ) : (
                    "授权本机"
                  )}
                </Button>
              )}
            </CardFooter>
          </form>
        </Card>
      </div>
    </div>
  );
}
