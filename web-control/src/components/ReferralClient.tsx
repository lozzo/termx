"use client";

import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Copy, Check, Users, Gift } from "lucide-react";

interface ReferralStats {
  referralCode: string;
  totalInvited: number;
  completedCount: number;
  rewardedDays: number;
  pendingDays: number;
}

interface ReferralClientProps {
  initialReferralStats: ReferralStats;
}

export default function ReferralClient({ initialReferralStats }: ReferralClientProps) {
  const referralCode = initialReferralStats.referralCode;
  const referralStats = initialReferralStats;
  const [referralCopied, setReferralCopied] = useState(false);

  const referralLink = referralCode
    ? `${typeof window !== "undefined" ? window.location.origin : ""}/register?ref=${referralCode}`
    : "";

  const handleCopyReferralLink = () => {
    if (referralLink) {
      navigator.clipboard.writeText(referralLink);
      setReferralCopied(true);
      setTimeout(() => setReferralCopied(false), 2000);
    }
  };

  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-3xl font-bold tracking-tight">邀请好友</h2>
        <p className="text-zinc-400">邀请好友注册并购买订阅，获得额外奖励。</p>
      </div>

      <Card className="bg-zinc-900 border-zinc-800">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Gift className="w-5 h-5" />
            邀请好友
          </CardTitle>
          <CardDescription>
            邀请好友注册并购买订阅，您将获得 3 天订阅奖励。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-2">
            <Label className="text-zinc-300">您的邀请链接</Label>
            <div className="flex items-center gap-2">
              <Input
                value={referralLink}
                readOnly
                className="bg-black border-zinc-800 text-white font-mono text-sm"
              />
              <Button
                variant="ghost"
                size="icon"
                onClick={handleCopyReferralLink}
                className="text-zinc-400 hover:text-primary shrink-0"
              >
                {referralCopied ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
              </Button>
            </div>
          </div>

          <div className="grid gap-2">
            <Label className="text-zinc-300">邀请码</Label>
            <code className="text-sm text-primary bg-black/50 px-3 py-2 rounded border border-zinc-800 inline-block">
              {referralCode || "加载中..."}
            </code>
          </div>

          <div className="grid grid-cols-2 gap-4 pt-2">
            <div className="p-3 rounded-lg border border-zinc-800 bg-zinc-950/50">
              <div className="flex items-center gap-2 text-zinc-400 text-xs mb-1">
                <Users className="w-3.5 h-3.5" />
                已邀请
              </div>
              <div className="text-xl font-bold text-white">{referralStats.totalInvited} <span className="text-sm font-normal text-zinc-500">人</span></div>
            </div>
            <div className="p-3 rounded-lg border border-zinc-800 bg-zinc-950/50">
              <div className="flex items-center gap-2 text-zinc-400 text-xs mb-1">
                <Gift className="w-3.5 h-3.5" />
                已获奖励
              </div>
              <div className="text-xl font-bold text-white">{referralStats.rewardedDays} <span className="text-sm font-normal text-zinc-500">天</span></div>
            </div>
            {referralStats.pendingDays > 0 && (
              <div className="col-span-2 p-3 rounded-lg border border-amber-500/20 bg-amber-500/5">
                <div className="text-sm text-amber-300">
                  待发放奖励: {referralStats.pendingDays} 天（购买订阅后自动叠加）
                </div>
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
