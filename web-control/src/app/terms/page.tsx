export const metadata = { title: '服务条款 - termx' }

export default function TermsPage() {
  return (
    <main className="max-w-3xl mx-auto px-6 py-16 text-zinc-300">
      <h1 className="text-3xl font-bold text-white mb-2">服务条款</h1>
      <p className="text-zinc-500 mb-10">最后更新：2026 年 3 月</p>

      <section className="space-y-8 text-sm leading-7">
        <div>
          <h2 className="text-white font-semibold text-base mb-2">1. 接受条款</h2>
          <p>
            使用 termx 服务即表示您同意本条款。如不同意，请停止使用本服务。
            我们保留随时修改本条款的权利，修改后继续使用视为接受。
          </p>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">2. 服务说明</h2>
          <p>
            termx 提供基于 P2P 加密的远程终端访问服务，包括客户端软件、中继服务器和管理控制台。
            服务按"现状"提供，我们不保证服务不中断或无错误。
          </p>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">3. 账户责任</h2>
          <ul className="list-disc list-inside space-y-1 text-zinc-400">
            <li>您负责维护账户凭据的安全，不得与他人共享账户</li>
            <li>您对账户下发生的所有活动负责</li>
            <li>发现未授权访问请立即联系我们</li>
          </ul>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">4. 禁止行为</h2>
          <p>您不得将本服务用于：</p>
          <ul className="list-disc list-inside mt-2 space-y-1 text-zinc-400">
            <li>任何违反中国法律法规或您所在地区法律的活动</li>
            <li>未经授权访问他人设备或系统</li>
            <li>传播恶意软件、病毒或有害内容</li>
            <li>干扰或破坏服务基础设施</li>
            <li>转售或商业转让服务访问权限</li>
          </ul>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">5. 订阅与付款</h2>
          <ul className="list-disc list-inside space-y-1 text-zinc-400">
            <li>订阅费用按周期预付，不支持部分退款</li>
            <li>到期前未取消将自动续订</li>
            <li>价格变动将提前 30 天通知</li>
            <li>因违反条款导致的账户封禁不予退款</li>
          </ul>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">6. 知识产权</h2>
          <p>
            termx 软件及服务的所有知识产权归我们所有。
            订阅期间我们授予您有限的、非独占的、不可转让的使用许可。
          </p>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">7. 免责声明</h2>
          <p>
            在法律允许的最大范围内，我们不对任何间接、偶发、特殊或后果性损失承担责任，
            包括但不限于数据丢失、业务中断或利润损失。
            我们对服务的总责任不超过您在过去 12 个月内支付的费用。
          </p>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">8. 服务终止</h2>
          <p>
            您可随时取消订阅并删除账户。我们保留因违反条款而暂停或终止账户的权利，
            情节严重者不另行通知。
          </p>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">9. 适用法律</h2>
          <p>
            本条款受中华人民共和国法律管辖。因本条款引起的争议，
            双方应首先协商解决；协商不成的，提交有管辖权的人民法院诉讼解决。
          </p>
        </div>

        <div>
          <h2 className="text-white font-semibold text-base mb-2">10. 联系我们</h2>
          <p>
            如有疑问，请发送邮件至{' '}
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
