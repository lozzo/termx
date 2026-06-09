"use client"

import React from "react";
import { Terminal, Shield, Zap, Smartphone, Lock, Code, Train, Moon, Sparkles, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { motion } from "framer-motion";
import Link from "next/link";
import Image from "next/image";
import { cn } from "@/lib/utils";
import { QRCodeSVG } from "qrcode.react";

import screenshot1 from "@/img/WechatIMG379.webp";
import screenshot2 from "@/img/WechatIMG380.webp";
import screenshot3 from "@/img/WechatIMG381.webp";
import screenshot4 from "@/img/WechatIMG382.webp";
import screenshot5 from "@/img/WechatIMG383.webp";

export default function LandingPage() {
  return (
    <div className="min-h-screen bg-black text-white selection:bg-primary selection:text-black font-sans relative">
      {/* Background Grid */}
      <div className="fixed inset-0 z-0 pointer-events-none">
        <div className="absolute inset-0 bg-[linear-gradient(to_right,#80808012_1px,transparent_1px),linear-gradient(to_bottom,#80808012_1px,transparent_1px)] bg-[size:24px_24px]"></div>
        <div className="absolute left-0 right-0 top-0 -z-10 m-auto h-[310px] w-[310px] rounded-full bg-primary/20 opacity-20 blur-[100px]"></div>
      </div>

      {/* Header */}
      <header className="fixed top-0 w-full z-50 border-b border-white/5 bg-black/50 backdrop-blur-md">
        <div className="container mx-auto px-4 h-16 flex items-center justify-between">
          <div className="flex items-center gap-2 font-mono font-bold text-xl tracking-tighter">
            <Terminal className="w-6 h-6 text-primary" />
            <span>termx</span>
          </div>
          <nav className="hidden md:flex items-center gap-8 text-sm font-medium text-zinc-400">
            <a href="#features" className="hover:text-white transition-colors">特性</a>
            <a href="#scenarios" className="hover:text-white transition-colors">场景</a>
            <a href="#security" className="hover:text-white transition-colors">安全</a>
            <a href="#pricing" className="hover:text-white transition-colors">定价</a>
            <a href="/docs" className="hover:text-white transition-colors">文档</a>
          </nav>
          <div className="flex items-center gap-4">
            <Link href="/dashboard">
              <Button variant="ghost" className="text-zinc-400 hover:text-white">
                控制台
              </Button>
            </Link>
            <a href="/api/v1/app/releases/latest?platform=android&type=apk&redirect=true">
              <Button className="bg-primary text-black hover:bg-primary/90 font-bold">
                下载 Android APK
              </Button>
            </a>
          </div>
        </div>
      </header>

      <main className="relative z-10">
        {/* Hero Section */}
        <section className="pt-32 pb-20 md:pt-48 md:pb-32 relative overflow-hidden">
          <div className="container mx-auto px-4 relative z-10">
            <div className="flex flex-col lg:flex-row items-center gap-12">
              <div className="flex-1 text-center lg:text-left">
                <motion.div
                  initial={{ opacity: 0, y: 20 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ duration: 0.5 }}
                >
                  <h1 className="text-5xl md:text-7xl font-bold tracking-tight mb-6 leading-tight">
                    你的终端。 <br />
                    <span className="text-zinc-500">跨越网络。</span> <br />
                    <span className="text-primary">无处不在。</span>
                  </h1>
                  <p className="text-xl text-zinc-400 mb-8 max-w-2xl mx-auto lg:mx-0 leading-relaxed">
                    下一代跨平台 TermX 远程终端。P2P 实时直连，零代码赋予任何 CLI 工具与 AI Agent 现代化的原生移动端操控体验。
                  </p>
                  <div className="flex flex-col sm:flex-row items-center gap-4 justify-center lg:justify-start">
                    <a href="/api/v1/app/releases/latest?platform=android&type=apk&redirect=true">
                      <Button size="lg" className="bg-primary text-black hover:bg-primary/90 font-bold h-12 px-8 text-lg w-full sm:w-auto">
                        下载 Android APK
                      </Button>
                    </a>
                    <Link href="/docs">
                      <Button size="lg" variant="outline" className="border-zinc-800 text-zinc-300 hover:bg-zinc-900 hover:text-white h-12 px-8 text-lg w-full sm:w-auto">
                        查看文档
                      </Button>
                    </Link>
                  </div>
                  <p className="mt-3 text-sm text-zinc-500 text-center lg:text-left">iOS 客户端暂未公开发布</p>
                  <div className="mt-8 flex items-center justify-center lg:justify-start gap-4 text-sm text-zinc-500 font-mono">
                    <span>$ curl -sL https://termx.omscd.com/install.sh | bash</span>
                  </div>
                </motion.div>
              </div>

              {/* Terminal Animation Mockup */}
              <div className="flex-1 w-full max-w-xl lg:max-w-none">
                <motion.div
                  initial={{ opacity: 0, scale: 0.95 }}
                  animate={{ opacity: 1, scale: 1 }}
                  transition={{ duration: 0.5, delay: 0.2 }}
                  className="relative rounded-xl overflow-hidden border border-zinc-800 bg-zinc-950 shadow-2xl"
                >
                  <div className="flex items-center gap-2 px-4 py-3 bg-zinc-900 border-b border-zinc-800">
                    <div className="w-3 h-3 rounded-full bg-red-500" />
                    <div className="w-3 h-3 rounded-full bg-yellow-500" />
                    <div className="w-3 h-3 rounded-full bg-green-500" />
                    <div className="ml-2 text-xs text-zinc-500 font-mono">user@server: ~</div>
                  </div>
                  <div className="p-6 font-mono text-sm md:text-base h-[300px] md:h-[400px] overflow-hidden">
                    <div className="text-green-500 mb-2">➜  ~ termx --listen :8080</div>
                    <div className="text-zinc-300 mb-2">[termx] Starting daemon on :8080</div>
                    <div className="text-zinc-300 mb-2">[termx] Password: a3f8k2m9</div>
                    <div className="text-zinc-300 mb-2">[termx] TermX runtime ready</div>
                    <div className="text-blue-400 mb-4">[termx] Scan QR code to connect:</div>

                    <div className="border-2 border-white p-2 w-32 h-32 bg-white mx-auto mb-4 flex items-center justify-center">
                      <QRCodeSVG value="https://termx.omscd.com" size={112} />
                    </div>

                    <div className="text-zinc-500 animate-pulse">Waiting for mobile connection...</div>
                  </div>
                </motion.div>
              </div>
            </div>
          </div>
        </section>

        {/* Features Section */}
        <section id="features" className="py-24 bg-transparent">
          <div className="container mx-auto px-4">
            <div className="text-center mb-16">
              <h2 className="text-3xl md:text-4xl font-bold mb-4">为什么选择 termx?</h2>
              <p className="text-zinc-400 max-w-2xl mx-auto">
                专为现代开发者工作流打造。无缝连接您的机器终端 与移动设备。
              </p>
            </div>

            <div className="grid md:grid-cols-3 gap-8">
              <FeatureCard
                icon={<Zap className="w-8 h-8 text-yellow-400" />}
                title="优先 P2P 直连"
                description="基于智能打洞技术。即使你的服务器深藏内网，无需公网地址，依然能建立极低延迟的实时连接。优先使用端到端直连，速度拉满。"
              />
              <FeatureCard
                icon={<Code className="w-8 h-8 text-blue-400" />}
                title="零代码 AI Agent"
                description="还在为你的 Vibecoding 脚本手写 UI 吗？零代码适配！只要能在 TermX 里跑的 CLI 或 Agent 进程，termx 立刻为您提供移动端可随身携带的管控界面。"
              />
              <FeatureCard
                icon={<Smartphone className="w-8 h-8 text-green-400" />}
                title="原生级移动端体验"
                description="告别在手机上痛苦敲击命令的历史。我们提供专为触屏优化的 Android 客户端，内置高频指令虚拟键盘与手势支持。"
              />
            </div>
          </div>
        </section>

        {/* Scenarios Section */}
        <section id="scenarios" className="py-24 border-t border-white/5 bg-zinc-900/20">
          <div className="container mx-auto px-4">
            <div className="text-center mb-16">
              <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-primary/10 border border-primary/20 text-sm text-primary mb-4">
                <Sparkles className="w-4 h-4" />
                <span>Vibecoding Anywhere</span>
              </div>
              <h2 className="text-3xl md:text-4xl font-bold mb-4">随时随地，保持心流</h2>
              <p className="text-zinc-400 max-w-2xl mx-auto">
                真正的移动端生产力工具，让编程不再局限于桌面。
              </p>
            </div>

            <div className="grid lg:grid-cols-3 gap-8">
              {/* Scenario 1: Commute */}
              <ScenarioCard
                icon={<Train className="w-6 h-6" />}
                title="随时随地连回 TermX"
                description="在地铁上突然想到优化方案？掏出手机，无缝接入工作机器的 TermX 终端，用你熟悉的 CLI 工具快速验证想法。"
                color="from-blue-500/20 to-purple-500/20"
              >
                <div className="font-mono text-[10px] leading-relaxed text-zinc-300">
                  <div className="flex gap-2 mb-1">
                    <span className="text-green-400">user@dev:~$</span>
                    <span>tail -n 2 slow.log</span>
                  </div>
                  <div className="text-zinc-500 mb-2">
                    [2024-03-20 08:01:23] QUERY_TIMEOUT: SELECT * FROM users WHERE...
                  </div>
                  <div className="flex gap-2 mb-1">
                    <span className="text-green-400">user@dev:~$</span>
                    <span>cat slow.log | llm &quot;fix this&quot;</span>
                  </div>
                  <div className="p-2 bg-zinc-800/50 rounded border border-white/5 mb-2">
                    <span className="text-zinc-400"># Suggested Index:</span><br/>
                    <span className="text-yellow-400">CREATE INDEX</span> idx_created_at <span className="text-yellow-400">ON</span> users(created_at);
                  </div>
                </div>
              </ScenarioCard>

              {/* Scenario 2: Bedtime */}
              <ScenarioCard
                icon={<Moon className="w-6 h-6" />}
                title="原生终端体验"
                description="告别简陋的 SSH 客户端。termx 提供完整的终端模拟，支持自定义快捷键栏，让你躺在沙发上也能流畅使用 Vim/Emacs。"
                color="from-primary/20 to-emerald-500/20"
              >
                <div className="font-mono text-[10px] leading-relaxed text-zinc-300 h-full flex flex-col">
                  <div className="bg-zinc-800 text-zinc-400 px-2 py-0.5 text-[8px] flex justify-between">
                    <span>weather_bot.py</span>
                    <span>Python</span>
                  </div>
                  <div className="flex-1 p-1">
                    <span className="text-purple-400">import</span> os<br/>
                    <span className="text-purple-400">import</span> requests<br/>
                    <br/>
                    <span className="text-purple-400">def</span> <span className="text-blue-400">main</span>():<br/>
                    &nbsp;&nbsp;api_key = os.getenv(<span className="text-green-400">&quot;API_KEY&quot;</span>)<br/>
                    &nbsp;&nbsp;<span className="animate-pulse">|</span>
                  </div>
                  <div className="border-t border-zinc-700 pt-1 mt-1 text-[8px] text-zinc-500">
                    -- INSERT --
                  </div>
                </div>
              </ScenarioCard>

              {/* Scenario 3: Emergency */}
              <ScenarioCard
                icon={<Zap className="w-6 h-6" />}
                title="机器状态一手掌握"
                description="服务报警心急如焚？无论身在何处，秒级直连服务器，像在电脑前一样使用 htop 排查问题，一键重启服务。"
                color="from-red-500/20 to-orange-500/20"
              >
                <div className="font-mono text-[10px] leading-relaxed text-zinc-300">
                  <div className="flex justify-between bg-zinc-800 px-1 text-[8px] mb-1">
                    <span>Tasks: 42</span><span>Load: 2.14</span><span>Uptime: 14d</span>
                  </div>
                  <div className="space-y-0.5 mb-2">
                    <div className="flex gap-1 text-[8px]">
                      <span className="text-red-400">PID</span>
                      <span className="text-zinc-400">USER</span>
                      <span className="text-red-400">CPU%</span>
                      <span className="text-zinc-400">MEM%</span>
                      <span className="text-zinc-300">COMMAND</span>
                    </div>
                    <div className="flex gap-1 text-[8px] bg-red-500/20">
                      <span className="text-red-400">892</span>
                      <span className="text-zinc-400">root</span>
                      <span className="text-red-400">98.2</span>
                      <span className="text-zinc-400">1.2</span>
                      <span className="text-zinc-300">/opt/miner</span>
                    </div>
                    <div className="flex gap-1 text-[8px]">
                      <span className="text-green-400">104</span>
                      <span className="text-zinc-400">www</span>
                      <span className="text-green-400">0.5</span>
                      <span className="text-zinc-400">4.1</span>
                      <span className="text-zinc-300">nginx: worker</span>
                    </div>
                  </div>
                  <div className="flex gap-2 mt-3 pt-2 border-t border-white/10">
                    <span className="text-green-400">root@prod:~#</span>
                    <span>kill -9 892</span>
                  </div>
                </div>
              </ScenarioCard>
            </div>
          </div>
        </section>

        {/* App Screenshots Showcase */}
        <section className="py-24 border-t border-white/5 overflow-hidden">
          <div className="container mx-auto px-4">
            <div className="text-center mb-16">
              <h2 className="text-3xl md:text-4xl font-bold mb-4">真机实拍</h2>
              <p className="text-zinc-400 max-w-2xl mx-auto">
                无论是 AI 编程助手、系统监控还是语音交互，termx 让一切 CLI 工具触手可及。
              </p>
            </div>
          </div>

          {/* Screenshots Fan Layout */}
          <div className="relative flex items-center justify-center gap-4 md:gap-6 px-4 overflow-x-auto md:overflow-visible pb-8 md:pb-0 snap-x snap-mandatory md:snap-none">
            {[
              { src: screenshot1, alt: "OpenCode 终端界面", rotate: "md:-rotate-6", scale: "md:scale-90", z: "z-10" },
              { src: screenshot4, alt: "htop 服务器监控", rotate: "md:-rotate-3", scale: "md:scale-95", z: "z-20" },
              { src: screenshot2, alt: "Claude Code 编辑调试", rotate: "md:rotate-0", scale: "md:scale-100", z: "z-30" },
              { src: screenshot3, alt: "命令面板与快捷键", rotate: "md:rotate-3", scale: "md:scale-95", z: "z-20" },
              { src: screenshot5, alt: "Gemini CLI 语音输入", rotate: "md:rotate-6", scale: "md:scale-90", z: "z-10" },
            ].map((item, i) => (
              <motion.div
                key={i}
                initial={{ opacity: 0, y: 40 }}
                whileInView={{ opacity: 1, y: 0 }}
                viewport={{ once: true, margin: "-50px" }}
                transition={{ duration: 0.5, delay: i * 0.1 }}
                className={cn(
                  "flex-shrink-0 snap-center relative group",
                  item.rotate,
                  item.scale,
                  item.z,
                  "hover:!rotate-0 hover:!scale-150 hover:!z-50 transition-transform duration-300 cursor-zoom-in"
                )}
              >
                <div className="w-[220px] md:w-[240px] rounded-[2rem] overflow-hidden border-2 border-zinc-700 bg-zinc-900 shadow-2xl group-hover:border-primary/50 transition-colors duration-300">
                  <Image
                    src={item.src}
                    alt={item.alt}
                    className="w-full h-auto"
                    placeholder="blur"
                  />
                </div>
                <p className="text-center text-xs text-zinc-500 mt-3 opacity-0 group-hover:opacity-100 transition-opacity">
                  {item.alt}
                </p>
              </motion.div>
            ))}
          </div>
        </section>

        {/* Security Section */}
        <section id="security" className="py-24 border-y border-white/5 bg-black/40 backdrop-blur-sm">
          <div className="container mx-auto px-4">
            <div className="flex flex-col md:flex-row items-center gap-12">
              <div className="flex-1">
                <Shield className="w-16 h-16 text-primary mb-6" />
                <h2 className="text-3xl md:text-4xl font-bold mb-6">隐私至上，安全第一。</h2>
                <div className="space-y-6">
                  <SecurityPoint
                    title="不存储敏感数据"
                    description="我们的 Hub 服务仅负责协助建立连接和计费。您的任何终端输入输出流绝对不会被保存在我们的服务器上。"
                  />
                  <SecurityPoint
                    title="端到端加密"
                    description="您的 Android 客户端与 Agent 之间的所有流量均经过加密。即使使用 Hub 中转，也只是安全的二进制流透传。"
                  />
                  <SecurityPoint
                    title="安全认证"
                    description="本地密码采用 bcrypt 强哈希算法加密。采用基于 HMAC (HS256) 签名的 JWT 进行无状态鉴权。"
                  />
                </div>
              </div>
              <div className="flex-1 bg-zinc-900/50 p-8 rounded-2xl border border-zinc-800">
                <pre className="font-mono text-xs md:text-sm text-zinc-400 overflow-x-auto">
{`// Security Architecture
{
  "encryption": "WebRTC DTLS + AES-256-GCM",
  "signing": "Ed25519",
  "auth": {
    "method": "JWT",
    "signature": "HS256"
  },
  "storage": {
    "logs": false,
    "session_recording": false
  }
}`}
                </pre>
              </div>
            </div>
          </div>
        </section>

        {/* Pricing Section */}
        <section id="pricing" className="py-24 bg-transparent">
          <div className="container mx-auto px-4">
             <div className="text-center mb-16">
              <h2 className="text-3xl md:text-4xl font-bold mb-4">简单透明的定价</h2>
              <p className="text-zinc-400 max-w-2xl mx-auto">
                本地直连永久免费。升级 Pro 订阅，享受全球高速中继。
              </p>
            </div>

            <div className="grid md:grid-cols-2 gap-8 max-w-4xl mx-auto">
              <PricingCard
                title="社区版"
                price="¥0"
                period="永久免费"
                features={[
                  "无限本地会话",
                  "P2P 直连",
                  "完整 Android 客户端访问",
                  "社区支持"
                ]}
                cta="免费下载"
                highlight={false}
              />
              <PricingCard
                title="Pro 版"
                price="¥9"
                period="/ 月"
                features={[
                  "包含社区版所有功能",
                  "全球高速 Hub 中继网络",
                  "100% 连接可靠性",
                  "云端节点状态看板",
                  "优先技术支持"
                ]}
                cta="升级 Pro"
                highlight={true}
                href="/dashboard/billing"
              />
            </div>
          </div>
        </section>

        {/* Footer */}
        <footer className="py-12 border-t border-white/5 bg-black/80 backdrop-blur-md text-zinc-500 text-sm">
          <div className="container mx-auto px-4">
            <div className="grid md:grid-cols-4 gap-8 mb-8">
              <div>
                <div className="flex items-center gap-2 font-mono font-bold text-white text-lg mb-4">
                  <Terminal className="w-5 h-5 text-primary" />
                  <span>termx</span>
                </div>
                <p>AI 时代的现代化终端。</p>
              </div>
              <div>
                <h4 className="font-bold text-white mb-4">产品</h4>
                <ul className="space-y-2">
                  <li><a href="#" className="hover:text-primary">下载</a></li>
                  <li><a href="#" className="hover:text-primary">文档</a></li>
                  <li><a href="#" className="hover:text-primary">更新日志</a></li>
                </ul>
              </div>
              <div>
                <h4 className="font-bold text-white mb-4">法律</h4>
                <ul className="space-y-2">
                  <li><a href="/privacy" className="hover:text-primary">隐私协议</a></li>
                  <li><a href="/terms" className="hover:text-primary">服务条款</a></li>
                </ul>
              </div>
            </div>
            <div className="text-center pt-8 border-t border-zinc-900">
              &copy; {new Date().getFullYear()} termx. 保留所有权利。
            </div>
          </div>
        </footer>
      </main>
    </div>
  );
}

function FeatureCard({ icon, title, description }: { icon: React.ReactNode, title: string, description: string }) {
  return (
    <Card className="bg-zinc-900/50 backdrop-blur-sm border-white/5 hover:border-primary/50 transition-colors">
      <CardHeader>
        <div className="mb-4">{icon}</div>
        <CardTitle className="text-xl text-white">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <CardDescription className="text-zinc-400 text-base leading-relaxed">
          {description}
        </CardDescription>
      </CardContent>
    </Card>
  )
}

function SecurityPoint({ title, description }: { title: string, description: string }) {
  return (
    <div>
      <h3 className="text-lg font-bold text-white mb-2 flex items-center gap-2">
        <Lock className="w-4 h-4 text-primary" />
        {title}
      </h3>
      <p className="text-zinc-400 leading-relaxed">{description}</p>
    </div>
  )
}

function PricingCard({ title, price, period, features, cta, highlight, href }: { title: string, price: string, period: string, features: string[], cta: string, highlight: boolean, href?: string }) {
  const button = (
    <Button className={cn(
      "w-full font-bold",
      highlight ? "bg-primary text-black hover:bg-primary/90" : "bg-zinc-800 text-white hover:bg-zinc-700"
    )}>
      {cta}
    </Button>
  );

  return (
    <Card className={cn(
      "bg-zinc-900/50 backdrop-blur-sm border-white/5 relative overflow-hidden",
      highlight && "border-primary shadow-[0_0_30px_rgba(16,185,129,0.1)]"
    )}>
      {highlight && (
        <div className="absolute top-0 right-0 bg-primary text-black text-xs font-bold px-3 py-1 rounded-bl-lg">
          推荐
        </div>
      )}
      <CardHeader>
        <CardTitle className="text-2xl text-white">{title}</CardTitle>
        <div className="flex items-baseline gap-1 mt-4">
          <span className="text-4xl font-bold text-white">{price}</span>
          <span className="text-zinc-500">{period}</span>
        </div>
      </CardHeader>
      <CardContent>
        <ul className="space-y-4 mb-8">
          {features.map((feature, i) => (
            <li key={i} className="flex items-center gap-3 text-zinc-300">
              <div className="w-1.5 h-1.5 rounded-full bg-primary" />
              {feature}
            </li>
          ))}
        </ul>
        {href ? <Link href={href}>{button}</Link> : button}
      </CardContent>
    </Card>
  )
}

function ScenarioCard({ icon, title, description, children, color }: { icon: React.ReactNode, title: string, description: string, children: React.ReactNode, color: string }) {
  return (
    <div className="group relative">
      <div className={cn("absolute -inset-0.5 bg-gradient-to-br rounded-[2.5rem] opacity-30 blur group-hover:opacity-70 transition duration-500", color)}></div>
      <div className="relative h-full bg-black rounded-[2.2rem] border border-zinc-800 overflow-hidden flex flex-col">
        {/* Phone Header/Notch area */}
        <div className="h-8 bg-zinc-900/50 border-b border-white/5 flex items-center justify-center px-6">
           <div className="w-16 h-4 bg-black rounded-b-xl border border-white/5 border-t-0"></div>
        </div>

        {/* Screen Content */}
        <div className="flex-1 p-6 bg-black/50 relative overflow-hidden">
          {/* Background Grid inside phone */}
          <div className="absolute inset-0 opacity-20 bg-[linear-gradient(to_right,#80808012_1px,transparent_1px),linear-gradient(to_bottom,#80808012_1px,transparent_1px)] bg-[size:12px_12px]"></div>

          <div className="relative z-10 h-full flex flex-col">
             {/* Dynamic Content passed as children */}
             <div className="flex-1">
               {children}
             </div>

             {/* Bottom Input Area Mockup */}
             <div className="mt-4 pt-3 border-t border-white/10">
               <div className="flex items-center gap-2">
                 <div className="w-6 h-6 rounded-full bg-zinc-800 flex items-center justify-center">
                   <Sparkles className="w-3 h-3 text-primary" />
                 </div>
                 <div className="h-2 w-24 bg-zinc-800 rounded-full"></div>
               </div>
             </div>
          </div>
        </div>

        {/* Card Text Content */}
        <div className="p-6 border-t border-white/5 bg-zinc-900/30 backdrop-blur-md">
          <div className="flex items-center gap-3 mb-3">
            <div className="p-2 rounded-lg bg-zinc-800 text-white">
              {icon}
            </div>
            <h3 className="text-lg font-bold text-white">{title}</h3>
          </div>
          <p className="text-sm text-zinc-400 leading-relaxed">
            {description}
          </p>
        </div>
      </div>
    </div>
  );
}
