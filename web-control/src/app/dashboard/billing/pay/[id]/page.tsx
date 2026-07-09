"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { useParams, useRouter } from "next/navigation";
import { QRCodeSVG } from "qrcode.react";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Loader2,
  CheckCircle2,
  XCircle,
  ArrowLeft,
  RefreshCw,
  Clock,
  ShieldCheck,
  Check,
  CreditCard,
} from "lucide-react";

// ─── Types ───────────────────────────────────────────────

type PayStatus = "idle" | "loading" | "paying" | "success" | "expired" | "error";

interface OrderInfo {
  id: string;
  planName: string;
  amount: number;
  originalAmount: number;
  discountAmount: number;
  currency: string;
  billingCycle: string;
  status: string;
  features: string[];
}

interface PaymentInfo {
  codeUrl: string;
  checkoutUrl: string;
  amount: number;
  expireTime: string;
}

// ─── SVG Icons ───────────────────────────────────────────

function WechatIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 1024 1024" xmlns="http://www.w3.org/2000/svg" fill="currentColor">
      <path d="M690.1 377.4c5.9 0 11.8.2 17.6.5-15.8-73.2-88.6-127.7-174.2-127.7-95.4 0-175.1 64.6-175.1 148.5 0 48.3 26.4 88 70.5 117.7l-17.6 52.9 61.7-30.9c22.1 4.4 39.7 8.8 61.7 8.8 5.7 0 11.3-.3 16.9-.8-3.5-12.2-5.5-24.9-5.5-38.1 0-72.7 64.6-130.9 144-130.9zM543.6 330c13.2 0 22.1 8.8 22.1 22.1 0 13.2-8.8 22.1-22.1 22.1-13.2 0-26.4-8.8-26.4-22.1 0-13.3 13.2-22.1 26.4-22.1zm-114.6 44.2c-13.2 0-26.4-8.8-26.4-22.1 0-13.2 13.2-22.1 26.4-22.1 13.2 0 22.1 8.8 22.1 22.1 0 13.2-8.8 22.1-22.1 22.1zm355.8 145.3c0-72.6-64.6-131-144.1-131s-144.1 58.4-144.1 131c0 72.7 64.6 131 144.1 131 17.6 0 35.3-4.4 52.9-8.8l48.4 26.4-13.2-44.2c35.3-26.5 56-61.8 56-105.4zm-192.1-22.1c-8.8 0-17.6-8.8-17.6-17.6 0-8.8 8.8-17.6 17.6-17.6s17.6 8.8 17.6 17.6c0 8.8-8.8 17.6-17.6 17.6zm96.2 0c-8.8 0-17.6-8.8-17.6-17.6 0-8.8 8.8-17.6 17.6-17.6s17.6 8.8 17.6 17.6c0 8.8-8.8 17.6-17.6 17.6z" />
    </svg>
  );
}

function AlipayIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 1024 1024" xmlns="http://www.w3.org/2000/svg" fill="currentColor">
      <path d="M789 610.3c-38.7-12.9-90.7-32.7-148.5-53.3 34.8-60.3 58.2-131.2 65.8-177.3h-172v-49.7h200.4v-36H534.3V192h-80.2s-4.3 0-4.3 5.2v97.5H238.6v36H450v49.7H296.8v36h233c-10.3 51.7-33.4 115.4-64.8 170-75.8-27.3-142-41.5-177.4-41.5C206.3 545 148 601.3 148 672.7c0 71.5 60.1 126.3 145.8 126.3 104.8 0 194.5-76.4 268.4-186.8 109.5 42.3 208.9 83.6 208.9 83.6L826 610.3h-37z m-494.8 146c-60.3 0-90.2-30-90.2-68 0-38.3 36.2-74.7 89.5-74.7 48 0 115.5 24.2 183.7 60.7-57.6 59.4-118.2 82-183 82z" />
    </svg>
  );
}

// ─── Page ────────────────────────────────────────────────

export default function PayPage() {
  const { id: orderId } = useParams<{ id: string }>();
  const router = useRouter();

  const [order, setOrder] = useState<OrderInfo | null>(null);
  const [paymentMethod, setPaymentMethod] = useState("wechat");
  const [payStatus, setPayStatus] = useState<PayStatus>("idle");
  const [payment, setPayment] = useState<PaymentInfo | null>(null);
  const [error, setError] = useState("");
  const [countdown, setCountdown] = useState("");

  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const countdownRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const cleanup = useCallback(() => {
    if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null; }
    if (countdownRef.current) { clearInterval(countdownRef.current); countdownRef.current = null; }
  }, []);

  useEffect(() => {
    fetch(`/api/billing/orders/${orderId}`)
      .then((r) => r.json())
      .then((data) => {
        if (data.error) { setError(data.error); setPayStatus("error"); return; }
        setOrder(data);
        if (data.status === "paid") setPayStatus("success");
      })
      .catch(() => { setError("加载订单失败"); setPayStatus("error"); });
    return cleanup;
  }, [orderId, cleanup]);

  const startCountdown = useCallback((expireTime: string) => {
    if (countdownRef.current) clearInterval(countdownRef.current);
    const tick = () => {
      const remaining = new Date(expireTime).getTime() - Date.now();
      if (remaining <= 0) { setCountdown("00:00"); setPayStatus("expired"); cleanup(); return; }
      const m = Math.floor(remaining / 60000);
      const s = Math.floor((remaining % 60000) / 1000);
      setCountdown(`${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`);
    };
    tick();
    countdownRef.current = setInterval(tick, 1000);
  }, [cleanup]);

  const startPolling = useCallback(() => {
    if (pollRef.current) clearInterval(pollRef.current);
    const poll = async () => {
      try {
        const res = await fetch(`/api/billing/orders/${orderId}/pay/status`);
        if (!res.ok) return;
        const data = await res.json();
        if (data.status === "paid") { cleanup(); setPayStatus("success"); }
        else if (data.status === "closed" || data.status === "expired") { cleanup(); setPayStatus("expired"); }
      } catch { /* retry */ }
    };
    pollRef.current = setInterval(poll, 3000);
  }, [orderId, cleanup]);

  const handlePay = useCallback(async () => {
    setPayStatus("loading");
    setError("");
    try {
      const res = await fetch(`/api/billing/orders/${orderId}/pay`, { method: "POST" });
      if (!res.ok) { const data = await res.json(); throw new Error(data.error || "发起支付失败"); }
      const data: PaymentInfo = await res.json();
      setPayment(data);
      setPayStatus("paying");
      startCountdown(data.expireTime);
      startPolling();
    } catch (err) {
      setError(err instanceof Error ? err.message : "发起支付失败");
      setPayStatus("error");
    }
  }, [orderId, startCountdown, startPolling]);

  const handleCancel = async () => {
    try { await fetch(`/api/billing/orders/${orderId}/cancel`, { method: "POST" }); } catch { /* */ }
    cleanup();
    router.push("/dashboard/billing");
  };

  const goBack = () => router.push("/dashboard/billing");
  const isPaying = payStatus === "paying";
  const amountYuan = order ? `¥${(order.amount / 100).toFixed(2)}` : "";
  const originalYuan = order && order.discountAmount > 0 ? `¥${(order.originalAmount / 100).toFixed(2)}` : "";
  const discountYuan = order && order.discountAmount > 0 ? `¥${(order.discountAmount / 100).toFixed(2)}` : "";
  const cycleName = order?.billingCycle === "yearly" ? "年度订阅" : "月度订阅";

  // ─── Success ──────────────────────────────────────────
  if (payStatus === "success") {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <div className="w-full max-w-lg text-center space-y-6">
          <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-primary/10">
            <CheckCircle2 className="w-10 h-10 text-primary" />
          </div>
          <div>
            <h1 className="text-2xl font-bold">支付成功</h1>
            <p className="text-zinc-400 mt-2">
              您已成功开通 {order?.planName ?? "Pro"} 订阅，远程访问能力现已生效。
            </p>
          </div>
          <Card className="bg-zinc-900/50 border-white/5">
            <CardContent className="py-5">
              <div className="flex items-center justify-between">
                <span className="text-zinc-400 text-sm">订阅状态</span>
                <Badge className="bg-primary/20 text-primary border-primary/30">已生效</Badge>
              </div>
            </CardContent>
          </Card>
          <Button
            className="w-full bg-primary text-black hover:bg-primary/90 font-bold h-11"
            onClick={goBack}
          >
            返回账单页面
          </Button>
        </div>
      </div>
    );
  }

  // ─── Main checkout ────────────────────────────────────
  return (
    <div className="max-w-lg mx-auto space-y-6">
      {/* Back + Title */}
      <div>
        <button
          onClick={goBack}
          className="inline-flex items-center text-zinc-500 hover:text-white transition-colors text-sm mb-3"
        >
          <ArrowLeft className="w-4 h-4 mr-1" />
          返回
        </button>
        <h1 className="text-2xl font-bold">完成订阅购买</h1>
      </div>

      {/* Single checkout card */}
      <Card className="bg-zinc-900/50 backdrop-blur-sm border-white/5 overflow-hidden">
        <CardContent className="p-0">

          {/* ── Section: Order summary ── */}
          <div className="p-6 space-y-4">
            {order ? (
              <>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="font-semibold text-white">{order.planName}</h3>
                    <p className="text-zinc-500 text-sm">{cycleName}</p>
                  </div>
                  <span className="text-2xl font-bold text-primary">{amountYuan}</span>
                </div>
                {order.discountAmount > 0 && (
                  <div className="space-y-1 text-sm">
                    <div className="flex items-center justify-between text-zinc-400">
                      <span>原价</span>
                      <span className="line-through">{originalYuan}</span>
                    </div>
                    <div className="flex items-center justify-between text-primary">
                      <span>优惠</span>
                      <span>-{discountYuan}</span>
                    </div>
                  </div>
                )}
                {order.features.length > 0 && (
                  <div className="grid grid-cols-2 gap-x-4 gap-y-1.5">
                    {order.features.map((f, i) => (
                      <div key={i} className="flex items-center gap-1.5 text-xs text-zinc-400">
                        <Check className="w-3.5 h-3.5 text-primary shrink-0" />
                        <span className="truncate">{f}</span>
                      </div>
                    ))}
                  </div>
                )}
              </>
            ) : payStatus !== "error" ? (
              <div className="flex justify-center py-6">
                <Loader2 className="w-5 h-5 animate-spin text-zinc-600" />
              </div>
            ) : null}
          </div>

          <div className="border-t border-white/5" />

          {/* ── Section: Payment method ── */}
          <div className="p-6 space-y-3">
            <h4 className="text-sm font-medium text-zinc-400">支付方式</h4>
            <RadioGroup
              value={paymentMethod}
              onValueChange={setPaymentMethod}
              className="grid grid-cols-3 gap-3"
              disabled={isPaying}
            >
              <div>
                <RadioGroupItem value="wechat" id="pm-wechat" className="peer sr-only" />
                <Label
                  htmlFor="pm-wechat"
                  className="flex flex-col items-center gap-1.5 rounded-lg border-2 border-zinc-800 bg-zinc-800/50 p-3 hover:bg-zinc-800 peer-data-[state=checked]:border-green-500 peer-data-[state=checked]:bg-green-500/5 peer-data-[state=checked]:text-green-400 cursor-pointer transition-all text-zinc-400"
                >
                  <WechatIcon className="h-5 w-5" />
                  <span className="text-xs">微信支付</span>
                </Label>
              </div>
              <div className="relative">
                <RadioGroupItem value="alipay" id="pm-alipay" className="peer sr-only" disabled />
                <Label
                  htmlFor="pm-alipay"
                  className="flex flex-col items-center gap-1.5 rounded-lg border-2 border-zinc-800/50 bg-zinc-800/30 p-3 opacity-40 cursor-not-allowed transition-all text-zinc-500"
                >
                  <AlipayIcon className="h-5 w-5" />
                  <span className="text-xs">支付宝</span>
                </Label>
                <span className="absolute -top-1.5 -right-1.5 bg-zinc-700 text-zinc-400 text-[10px] leading-none px-1.5 py-0.5 rounded-full">
                  即将支持
                </span>
              </div>
              <div className="relative">
                <RadioGroupItem value="card" id="pm-card" className="peer sr-only" disabled />
                <Label
                  htmlFor="pm-card"
                  className="flex flex-col items-center gap-1.5 rounded-lg border-2 border-zinc-800/50 bg-zinc-800/30 p-3 opacity-40 cursor-not-allowed transition-all text-zinc-500"
                >
                  <CreditCard className="h-5 w-5" />
                  <span className="text-xs">银行卡</span>
                </Label>
                <span className="absolute -top-1.5 -right-1.5 bg-zinc-700 text-zinc-400 text-[10px] leading-none px-1.5 py-0.5 rounded-full">
                  即将支持
                </span>
              </div>
            </RadioGroup>
          </div>

          <div className="border-t border-white/5" />

          {/* ── Section: Action area ── */}
          <div className="p-6">
            {/* Idle */}
            {payStatus === "idle" && order && (
              <div className="space-y-4">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-zinc-400">应付总额</span>
                  <span className="text-xl font-bold text-white">{amountYuan}</span>
                </div>
                <Button
                  className="w-full bg-primary text-black hover:bg-primary/90 font-bold h-11"
                  onClick={handlePay}
                >
                  立即支付
                </Button>
                <p className="text-center text-[11px] text-zinc-600 flex items-center justify-center gap-1">
                  <ShieldCheck className="w-3 h-3" />
                  安全加密支付，随时可取消
                </p>
              </div>
            )}

            {/* Loading */}
            {payStatus === "loading" && (
              <div className="flex flex-col items-center py-6 gap-3">
                <Loader2 className="w-7 h-7 animate-spin text-primary" />
                <span className="text-sm text-zinc-500">正在创建支付订单...</span>
              </div>
            )}

            {/* QR Code */}
            {isPaying && payment && (
              <div className="flex flex-col items-center space-y-4">
                <p className="text-sm text-zinc-400">请使用微信扫描二维码完成支付</p>
                <div className="bg-white rounded-lg p-3 shadow-lg shadow-black/20">
                  <QRCodeSVG value={payment.codeUrl} size={180} level="M" marginSize={1} />
                </div>
                <div className="flex items-center gap-4 text-sm">
                  <div className="flex items-center gap-1.5 text-zinc-400">
                    <Clock className="w-3.5 h-3.5" />
                    <span className="text-white font-mono font-semibold">{countdown}</span>
                  </div>
                  <div className="w-px h-3.5 bg-zinc-700" />
                  <div className="flex items-center gap-1.5 text-zinc-500">
                    <Loader2 className="w-3 h-3 animate-spin" />
                    <span className="text-xs">等待支付...</span>
                  </div>
                </div>
                <button
                  onClick={handleCancel}
                  className="text-xs text-zinc-600 hover:text-zinc-400 transition-colors"
                >
                  取消支付
                </button>
              </div>
            )}

            {/* Expired */}
            {payStatus === "expired" && (
              <div className="flex flex-col items-center py-2 space-y-4">
                <div className="inline-flex items-center justify-center w-12 h-12 rounded-full bg-zinc-800">
                  <XCircle className="w-6 h-6 text-zinc-500" />
                </div>
                <p className="text-sm text-zinc-400">二维码已失效</p>
                <div className="flex gap-3 w-full">
                  <Button variant="outline" className="flex-1 h-10" onClick={goBack}>
                    <ArrowLeft className="w-4 h-4 mr-1.5" />
                    返回
                  </Button>
                  <Button className="flex-1 h-10 bg-primary text-black hover:bg-primary/90 font-bold" onClick={handlePay}>
                    <RefreshCw className="w-4 h-4 mr-1.5" />
                    重新支付
                  </Button>
                </div>
              </div>
            )}

            {/* Error */}
            {payStatus === "error" && (
              <div className="flex flex-col items-center py-2 space-y-4">
                <div className="inline-flex items-center justify-center w-12 h-12 rounded-full bg-destructive/10">
                  <XCircle className="w-6 h-6 text-destructive" />
                </div>
                <p className="text-sm text-zinc-400">{error}</p>
                <div className="flex gap-3 w-full">
                  <Button variant="outline" className="flex-1 h-10" onClick={goBack}>
                    <ArrowLeft className="w-4 h-4 mr-1.5" />
                    返回
                  </Button>
                  <Button className="flex-1 h-10 bg-primary text-black hover:bg-primary/90 font-bold" onClick={handlePay}>
                    <RefreshCw className="w-4 h-4 mr-1.5" />
                    重试
                  </Button>
                </div>
              </div>
            )}
          </div>

        </CardContent>
      </Card>
    </div>
  );
}
