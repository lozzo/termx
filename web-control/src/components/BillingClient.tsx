"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Card, CardContent, CardHeader, CardTitle, CardDescription, CardFooter } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Check, Zap, Loader2, Tag } from "lucide-react";
import { Switch } from "@/components/ui/switch";

interface PlanInfo {
  id: string;
  name: string;
  price: number;
  priceYearly: number;
  features: string[];
  maxServers: number;
  maxAgents: number;
}

interface SubscriptionInfo {
  id: string;
  planId: string;
  planName: string;
  status: string;
  currentPeriodEnd: string;
  billingCycle: string;
}

interface PromoResult {
  valid: boolean;
  discountType?: string;
  discountValue?: number;
  discountAmount?: number;
  finalAmount?: number;
  error?: string;
}

interface BillingClientProps {
  initialPlans: PlanInfo[];
  initialSubscription: SubscriptionInfo | null;
}

export default function BillingClient({ initialPlans, initialSubscription }: BillingClientProps) {
  const router = useRouter();
  const [isYearly, setIsYearly] = useState(false);
  const plans = initialPlans;
  const subscription = initialSubscription;
  const [upgrading, setUpgrading] = useState(false);

  // 优惠码状态
  const [promoCode, setPromoCode] = useState("");
  const [promoResult, setPromoResult] = useState<PromoResult | null>(null);
  const [validating, setValidating] = useState(false);

  const currentPlanName = subscription?.planName || "免费社区版";
  const isMonthly = subscription?.billingCycle === "monthly";
  const isYearlySubscriber = subscription?.billingCycle === "yearly";
  const hasSubscription = !!subscription;
  const billingDisabled = subscription?.planId === "dev";

  const proPlan = plans.find((p) => p.price > 0);
  const cycle = isMonthly ? "yearly" : (isYearly ? "yearly" : "monthly");

  const handleValidatePromo = async () => {
    if (!promoCode.trim() || !proPlan) return;
    setValidating(true);
    setPromoResult(null);
    try {
      const res = await fetch("/api/billing/promo-codes/validate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          code: promoCode.trim(),
          planId: proPlan.id,
          billingCycle: cycle,
        }),
      });
      const data: PromoResult = await res.json();
      setPromoResult(data);
    } catch {
      setPromoResult({ valid: false, error: "验证失败，请重试" });
    } finally {
      setValidating(false);
    }
  };

  const handleUpgrade = async () => {
    if (billingDisabled) return;
    if (!proPlan) return;

    setUpgrading(true);
    try {
      const body: Record<string, string> = {
        planId: proPlan.id,
        billingCycle: cycle,
      };
      if (promoResult?.valid && promoCode.trim()) {
        body.promoCode = promoCode.trim();
      }

      const res = await fetch("/api/billing/orders", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });

      if (!res.ok) {
        const data = await res.json();
        throw new Error(data.error || "创建订单失败");
      }

      const order = await res.json();
      if (order.paid) {
        router.refresh();
        return;
      }
      router.push(`/dashboard/billing/pay/${order.id}`);
    } catch (err) {
      alert(err instanceof Error ? err.message : "创建订单失败");
      setUpgrading(false);
    }
  };

  // 切换周期时清除优惠码验证结果
  const handleCycleChange = (yearly: boolean) => {
    setIsYearly(yearly);
    setPromoResult(null);
  };

  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-3xl font-bold tracking-tight">账单与订阅</h2>
        <p className="text-zinc-400">
          {billingDisabled ? "开发模式下已跳过支付，功能可直接使用。" : "管理您的当前订阅、计费周期和支付订单。"}
        </p>
      </div>

      {billingDisabled && (
        <Card className="bg-primary/5 border-primary/30">
          <CardHeader>
            <CardTitle className="text-primary">免支付运行模式</CardTitle>
            <CardDescription>
              当前环境默认跳过支付和订阅校验，所有远程连接能力都可以直接测试。
            </CardDescription>
          </CardHeader>
        </Card>
      )}

      {/* Current Plan */}
      <Card className="bg-zinc-900 border-zinc-800">
        <CardHeader>
          <div className="flex items-center justify-between">
            <div className="space-y-1">
              <CardTitle>当前计划</CardTitle>
              <CardDescription>您当前正在使用 <span className="text-white font-bold">{currentPlanName}</span>。</CardDescription>
            </div>
            <Badge variant="outline" className="border-zinc-700 text-zinc-400">
              {subscription ? subscription.status : "免费版"}
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between text-sm">
            <span className="text-zinc-400">计费周期</span>
            <span className="text-white">
              {billingDisabled ? "免支付" : subscription ? (subscription.billingCycle === "yearly" ? "年付" : "月付") : "N/A"}
            </span>
          </div>
          <div className="flex items-center justify-between text-sm">
            <span className="text-zinc-400">下次扣费</span>
            <span className="text-white">
              {subscription?.currentPeriodEnd
                ? new Date(subscription.currentPeriodEnd).toLocaleDateString()
                : "永不"}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* Upgrade Options */}
      <div className="space-y-6">
        {!hasSubscription && (
          <div className="flex items-center justify-center gap-4">
            <span className={`text-sm ${!isYearly ? "text-white font-bold" : "text-zinc-500"}`}>月付</span>
            <Switch checked={isYearly} onCheckedChange={handleCycleChange} />
            <span className={`text-sm ${isYearly ? "text-white font-bold" : "text-zinc-500"}`}>年付 (省 20%)</span>
          </div>
        )}

        <div className="grid md:grid-cols-2 gap-8">
          {/* Free Plan */}
          <Card className="bg-zinc-900 border-zinc-800">
            <CardHeader>
              <CardTitle>社区版</CardTitle>
              <div className="text-2xl font-bold mt-2">$0 <span className="text-sm font-normal text-zinc-500">/ 永久</span></div>
            </CardHeader>
            <CardContent>
              <ul className="space-y-3 text-sm text-zinc-400">
                <li className="flex items-center gap-2"><Check className="w-4 h-4" /> 无限本地会话</li>
                <li className="flex items-center gap-2"><Check className="w-4 h-4" /> P2P 直连</li>
                <li className="flex items-center gap-2"><Check className="w-4 h-4" /> 社区支持</li>
              </ul>
            </CardContent>
            <CardFooter>
              <Button disabled className="w-full bg-zinc-800">
                {!hasSubscription ? "当前计划" : "免费版"}
              </Button>
            </CardFooter>
          </Card>

          {/* Pro Plan */}
          <Card className="bg-zinc-900 border-primary shadow-[0_0_30px_rgba(16,185,129,0.1)] relative overflow-hidden">
            <div className="absolute top-0 right-0 bg-primary text-black text-xs font-bold px-3 py-1 rounded-bl-lg">
              {billingDisabled ? "已启用" : !hasSubscription ? "推荐" : isMonthly ? "当前计划 · 月付" : "当前计划 · 年付"}
            </div>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                Pro 版
                <Zap className="w-4 h-4 text-yellow-400 fill-yellow-400" />
              </CardTitle>
              <div className="text-2xl font-bold mt-2">
                {billingDisabled ? (
                  <>$0<span className="text-sm font-normal text-zinc-500"> / 开发</span></>
                ) : hasSubscription ? (
                  isMonthly ? (
                    <>$99<span className="text-sm font-normal text-zinc-500"> / 年</span></>
                  ) : (
                    <>$99<span className="text-sm font-normal text-zinc-500"> / 年</span></>
                  )
                ) : (
                  <>{isYearly ? "$99" : "$9"}<span className="text-sm font-normal text-zinc-500">{isYearly ? " / 年" : " / 月"}</span></>
                )}
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              <ul className="space-y-3 text-sm text-zinc-300">
                <li className="flex items-center gap-2"><Check className="w-4 h-4 text-primary" /> 全球高速 Hub 中继网络</li>
                <li className="flex items-center gap-2"><Check className="w-4 h-4 text-primary" /> 100% 连接可靠性</li>
                <li className="flex items-center gap-2"><Check className="w-4 h-4 text-primary" /> 云端节点状态看板</li>
                <li className="flex items-center gap-2"><Check className="w-4 h-4 text-primary" /> 优先技术支持</li>
              </ul>

              {/* 优惠码输入 */}
              {!billingDisabled && !isYearlySubscriber && (
                <div className="pt-2 border-t border-zinc-800">
                  <div className="flex items-center gap-2 mb-2">
                    <Tag className="w-3.5 h-3.5 text-zinc-500" />
                    <span className="text-xs text-zinc-500">优惠码</span>
                  </div>
                  <div className="flex gap-2">
                    <Input
                      placeholder="输入优惠码"
                      value={promoCode}
                      onChange={(e) => {
                        setPromoCode(e.target.value.toUpperCase());
                        setPromoResult(null);
                      }}
                      className="h-8 text-sm bg-zinc-800 border-zinc-700"
                    />
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 shrink-0"
                      onClick={handleValidatePromo}
                      disabled={!promoCode.trim() || validating}
                    >
                      {validating ? <Loader2 className="w-3 h-3 animate-spin" /> : "验证"}
                    </Button>
                  </div>
                  {promoResult && (
                    <div className={`mt-2 text-xs ${promoResult.valid ? "text-primary" : "text-red-400"}`}>
                      {promoResult.valid
                        ? `优惠 ¥${((promoResult.discountAmount ?? 0) / 100).toFixed(2)}，实付 ¥${((promoResult.finalAmount ?? 0) / 100).toFixed(2)}`
                        : promoResult.error}
                    </div>
                  )}
                </div>
              )}
            </CardContent>
            <CardFooter>
              {billingDisabled ? (
                <Button disabled className="w-full bg-zinc-800">
                  已启用
                </Button>
              ) : isYearlySubscriber ? (
                <Button disabled className="w-full bg-zinc-800">
                  当前计划
                </Button>
              ) : (
                <Button
                  className="w-full bg-primary text-black hover:bg-primary/90 font-bold"
                  onClick={handleUpgrade}
                  disabled={upgrading}
                >
                  {upgrading && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
                  {isMonthly ? "升级为年付" : "升级到 Pro"}
                </Button>
              )}
            </CardFooter>
          </Card>
        </div>
      </div>
    </div>
  );
}
