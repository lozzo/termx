export const metadata = { title: '隐私协议 - termx' }

export default function PrivacyPage() {
  return (
    <main className="max-w-3xl mx-auto px-6 py-16 text-zinc-300">
      <h1 className="text-3xl font-bold text-white mb-2">隐私协议</h1>
      <p className="text-zinc-500 mb-10">最后更新：2026 年 3 月</p>

      <section className="space-y-8 text-sm leading-7">
        <div>
          <h2 className="text-white font-semibold text-base mb-2">1. 我们收集哪些信息</h2>
          <p>我们仅收集提供服务所必需的最少信息，包括：</p>
          <ul className="list-disc list-inside mt-2 space-y-1 text-zinc-400">
            <li>注册时提供的邮箱地址和密码（密码经哈希处理后存储）</li>
            <li>设备连接所需的公钥和设备标识符</li>
            <li>服务使用日志（连接时间、流量统计），用于计费和故障排查</li>
          </ul>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">2. 我们如何使用信息</h2>
          <ul className="list-disc list-inside space-y-1 text-zinc-400">
            <li>提供、维护和改进 termx 服务</li>
            <li>处理订阅和支付</li>
            <li>发送服务相关通知（如密码重置、账单提醒）</li>
            <li>检测和防止滥用行为</li>
          </ul>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">3. 数据传输与存储</h2>
          <p>
            termx 采用端对端加密架构。您的终端会话数据通过 WebRTC 直接在您的设备之间传输，
            中继服务器仅在直连不可用时转发加密流量，<strong className="text-white">无法解密您的会话内容</strong>。
            账户数据存储在中国大陆的服务器上，受中国相关法律法规保护。
          </p>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">4. 信息共享</h2>
          <p>
            我们不会出售、出租或以任何商业目的共享您的个人信息。以下情况除外：
          </p>
          <ul className="list-disc list-inside mt-2 space-y-1 text-zinc-400">
            <li>您明确授权的情况</li>
            <li>法律法规要求或政府机关依法要求</li>
            <li>为提供服务而必须委托的第三方服务商（如支付处理），且受保密协议约束</li>
          </ul>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">5. 数据保留</h2>
          <p>
            账户数据在您主动删除账户后 30 天内从我们的系统中清除。
            服务日志保留不超过 90 天。
          </p>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">6. 您的权利</h2>
          <p>您有权随时：</p>
          <ul className="list-disc list-inside mt-2 space-y-1 text-zinc-400">
            <li>访问和导出您的账户数据</li>
            <li>更正不准确的个人信息</li>
            <li>删除您的账户及相关数据</li>
          </ul>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">7. Cookie</h2>
          <p>
            我们仅使用维持登录状态所必需的 Session Cookie，不使用任何追踪或广告 Cookie。
          </p>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">8. 联系我们</h2>
          <p>
            如有隐私相关问题，请发送邮件至{' '}
            <a href="mailto:support@omscd.com" className="text-primary hover:underline">
              support@omscd.com
            </a>
            。
          </p>
        </div>
      </section>
    </main>
  )
}
