import { BookOpen, Cloud, GitFork, Laptop, Search, Server, ShieldCheck, Smartphone, TerminalSquare } from 'lucide-react'
import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router'
import logo from '../../../../clients/mobile/android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png'

const sections = [
  { id: 'start', title: '快速开始', description: '构建 CLI、启动 daemon，并打开 TUI。', keywords: '安装 构建 daemon tui start build' },
  { id: 'routes', title: '连接路径', description: 'Local、SSH、Direct 与 Cloud 的边界和选择方式。', keywords: 'local ssh direct cloud p2p relay endpoint route' },
  { id: 'pairing', title: '扫码配对', description: '创建一次性二维码，把目标服务加入移动端。', keywords: '扫码 二维码 app mobile pair pairing qr' },
  { id: 'terminal', title: '终端与历史', description: '创建终端、实时画面、历史模式、搜索与复制。', keywords: 'terminal live history search copy scrollback' },
  { id: 'cloud', title: 'Cloud 接入', description: '注册 daemon，理解 Controller 与 Edge 的职责。', keywords: 'controller edge enroll cloud lifecycle block delete' },
  { id: 'security', title: '安全边界', description: '身份、授权、配对和终端数据分别由谁处理。', keywords: 'security identity access grant encryption 安全 权限' },
  { id: 'diagnostics', title: '诊断', description: '检查路径、daemon 状态、连接路由与日志。', keywords: 'doctor status logs config endpoint test 排障 日志' },
] as const

function Command({ children }: { children: string }) {
  return <pre className="docs-command" tabIndex={0}><code>{children}</code></pre>
}

export function DocsPage() {
  const location = useLocation()
  const mainRef = useRef<HTMLElement>(null)
  const [query, setQuery] = useState('')
  const matches = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase('zh-CN')
    if (!needle) return sections
    return sections.filter((section) => `${section.title} ${section.description} ${section.keywords}`.toLocaleLowerCase('zh-CN').includes(needle))
  }, [query])

  useLayoutEffect(() => {
    document.title = '使用文档 · AnyTTY Cloud'
    mainRef.current?.focus({ preventScroll: true })
  }, [location.pathname])

  return <div className="docs-page">
    <a className="skip-link" href="#docs-content">跳到文档正文</a>
    <header className="landing-header">
      <Link className="landing-brand" to="/"><img src={logo} alt="AnyTTY" /><strong>AnyTTY Cloud</strong></Link>
      <nav aria-label="公开导航">
        <Link to="/">首页</Link>
        <Link aria-current="page" to="/docs">文档</Link>
        <a href="https://github.com/anytty/anytty" aria-label="在 GitHub 查看 AnyTTY"><GitFork size={19} /></a>
        <Link className="button button-primary" to="/login">登录 Cloud</Link>
      </nav>
    </header>

    <main id="docs-content" ref={mainRef} tabIndex={-1}>
      <section className="docs-intro" aria-labelledby="docs-title">
        <span className="eyebrow">ANYTTY DOCUMENTATION</span>
        <h1 id="docs-title">AnyTTY 使用文档</h1>
        <p>从本机 daemon 到扫码配对和 Cloud 路由，这里只描述当前代码已经实现的路径。AnyTTY App 不登录、不自动发现设备；添加端点必须由用户扫描目标服务生成的二维码。</p>
        <label className="docs-search" htmlFor="docs-search">
          <Search size={19} aria-hidden="true" />
          <span className="visually-hidden">搜索文档主题</span>
          <input id="docs-search" type="search" aria-label="搜索文档主题" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索：扫码、SSH、历史、Cloud..." />
          <small aria-live="polite">{matches.length} 个主题</small>
        </label>
        {query && <nav className="docs-search-results" aria-label="搜索结果">
          {matches.length ? matches.map((section) => <a key={section.id} href={`#${section.id}`}><strong>{section.title}</strong><span>{section.description}</span></a>) : <p>没有匹配的主题。可以尝试“扫码”“SSH”或“历史”。</p>}
        </nav>}
      </section>

      <nav className="docs-route-rail" aria-label="连接路径速览">
        <a href="#routes"><Laptop size={21} /><span><b>Local</b><small>同一台电脑</small></span></a>
        <i aria-hidden="true" />
        <a href="#routes"><Server size={21} /><span><b>SSH / Direct</b><small>自管网络</small></span></a>
        <i aria-hidden="true" />
        <a href="#cloud"><Cloud size={21} /><span><b>Cloud</b><small>P2P / Relay</small></span></a>
      </nav>

      <div className="docs-layout">
        <aside>
          <strong>本页目录</strong>
          <nav aria-label="文档目录">{sections.map((section) => <a key={section.id} href={`#${section.id}`}>{section.title}</a>)}</nav>
          <p><BookOpen size={16} />项目仍在开发，命令和协议以当前 `master` 为准。</p>
        </aside>

        <article className="docs-article">
          <section id="start">
            <span className="docs-section-index">01</span><h2>快速开始</h2>
            <p>从源码构建后，先启动当前用户的 daemon，再进入 TUI。所有构建产物写入 `.artifacts/`。</p>
            <Command>{`npm ci\nmake build\n./.artifacts/bin/anytty daemon start\n./.artifacts/bin/anytty`}</Command>
            <p>偏好显式命令时，可以创建终端并立即附加：</p>
            <Command>{`./.artifacts/bin/anytty new --attach -- zsh`}</Command>
          </section>

          <section id="routes">
            <span className="docs-section-index">02</span><h2>连接路径</h2>
            <p>一个 endpoint 可以保存多条 route。客户端按策略选择可用路径；权限最终仍由目标 daemon 校验。</p>
            <dl className="docs-route-table">
              <div><dt>Local</dt><dd>通过当前用户的 Unix socket 连接本机 daemon。</dd></div>
              <div><dt>SSH</dt><dd>使用 OpenSSH 通道到达远端 daemon 的 loopback signaling 与 ICE-TCP 端口。</dd></div>
              <div><dt>Direct</dt><dd>直接连接 daemon 发布的 signaling 与 ICE-TCP 地址，不依赖 Cloud。</dd></div>
              <div><dt>Cloud</dt><dd>通过已注册 Edge 做信令，优先 P2P，必要时使用受配额约束的 Relay。</dd></div>
            </dl>
            <Command>{`ENDPOINT_ID=REPLACE_WITH_ENDPOINT_ID\n./.artifacts/bin/anytty endpoint list\n./.artifacts/bin/anytty endpoint show "$ENDPOINT_ID"\n./.artifacts/bin/anytty endpoint test "$ENDPOINT_ID"\n./.artifacts/bin/anytty endpoint policy show "$ENDPOINT_ID"`}</Command>
          </section>

          <section id="pairing">
            <span className="docs-section-index">03</span><h2>扫码配对</h2>
            <div className="docs-callout"><Smartphone size={22} /><p><strong>移动端只有扫码入口。</strong>它不使用 Cloud 账号，也不会自动发现或同步设备。</p></div>
            <p>在目标 daemon 所在电脑上创建十分钟有效、仅可使用一次的二维码，然后在 App 中扫描：</p>
            <Command>{`./.artifacts/bin/anytty pair create --qr-file ./anytty-pair.png`}</Command>
            <p>二维码只负责把目标身份、可用连接路径和一次性 claim 交给客户端。配对成功后，端点和客户端凭据保存在当前设备；二维码过期或被其他客户端消费时必须重新生成。原客户端若只丢失交付响应，可在 24 小时 delivery grace 内幂等恢复。</p>
          </section>

          <section id="terminal">
            <span className="docs-section-index">04</span><h2>终端与历史</h2>
            <p>Live 模式使用 protocol session 内的短期终端基线：基线可用时 daemon 返回增量，基线过期或出现 gap 时返回全量。客户端把当前 revision 提交给唯一 renderer 后立即重挂 long-poll；渲染期间只合并最新 damage，不排队无效画面。</p>
            <p>进入历史模式时会冻结进入瞬间的可见锚点，新输出继续写入历史但不会推动当前视口。滚动到最新位置会自动退出历史模式。搜索和大范围复制使用逻辑行范围，不预先把全部文本复制到前端内存。</p>
            <Command>{`TERMINAL_ID=REPLACE_WITH_TERMINAL_ID\n./.artifacts/bin/anytty ls\n./.artifacts/bin/anytty attach "$TERMINAL_ID"`}</Command>
            <p>手工执行历史保留策略前必须先停止 daemon：</p>
            <Command>{`./.artifacts/bin/anytty daemon stop\n./.artifacts/bin/anytty history prune\n./.artifacts/bin/anytty daemon start`}</Command>
          </section>

          <section id="cloud">
            <span className="docs-section-index">05</span><h2>Cloud 接入</h2>
            <p>Cloud 账号只管理 daemon 注册、Edge 路由、Relay、订阅与运营配置，不是 App 账号。登录 Cloud 控制台生成一次性 enrollment code 后，在目标电脑执行：</p>
            <Command>{`ENROLLMENT_CODE=REPLACE_WITH_ONE_TIME_CODE\n./.artifacts/bin/anytty cloud enroll --controller https://cloud.anytty.com "$ENROLLMENT_CODE"\n./.artifacts/bin/anytty daemon restart\n./.artifacts/bin/anytty pair create --route cloud --qr-file ./anytty-cloud-pair.png`}</Command>
            <p>Controller 保存账号、注册和策略真值；Edge 保存在线会话与策略的内存投影，并提供公网信令和 Relay；daemon 持有终端、文件、设备身份与客户端授权。阻断 daemon 会拒绝并关闭 Cloud 会话但允许恢复；删除会清除 Cloud enrollment，再次接入必须重新注册。</p>
          </section>

          <section id="security">
            <span className="docs-section-index">06</span><h2>安全边界</h2>
            <ul className="docs-checklist">
              <li><ShieldCheck size={18} />终端与文件权限由 daemon 的 AccessStore 和 CapabilityGrant 决定。</li>
              <li><ShieldCheck size={18} />Controller 不转发 terminal、file、SDP、ICE 或 CapabilityGrant 数据。</li>
              <li><ShieldCheck size={18} />Edge 负责准入和转发，不能提升客户端权限，也不能读取端到端终端内容。</li>
              <li><ShieldCheck size={18} />AgentGateway 与 ClientGateway 都使用 Edge 先发 challenge，握手 proof 绑定连接上下文。</li>
              <li><ShieldCheck size={18} />私钥、完整凭据、配对 claim 和终端内容不得进入日志。</li>
            </ul>
          </section>

          <section id="diagnostics">
            <span className="docs-section-index">07</span><h2>诊断</h2>
            <p>先确认实际路径和 daemon 状态，再测试 endpoint。不要把路由失败直接解释成 YAML 格式变化。</p>
            <Command>{`ENDPOINT_ID=REPLACE_WITH_ENDPOINT_ID\n./.artifacts/bin/anytty config paths\n./.artifacts/bin/anytty config validate\n./.artifacts/bin/anytty daemon doctor\n./.artifacts/bin/anytty daemon status\n./.artifacts/bin/anytty daemon logs\n./.artifacts/bin/anytty endpoint test "$ENDPOINT_ID"`}</Command>
            <div className="docs-next"><TerminalSquare size={22} /><div><strong>开发与部署</strong><p>完整仓库构建、Android 门禁和 Cloud 运维步骤见 <a href="https://github.com/anytty/anytty#readme">GitHub README</a> 与 <a href="https://github.com/anytty/anytty/blob/master/cloud/deploy/README.md">Cloud 部署文档</a>。</p></div></div>
          </section>
        </article>
      </div>
    </main>
    <footer className="landing-footer"><span>AnyTTY Cloud · Development</span><Link to="/">首页</Link><a href="https://github.com/anytty/anytty">GitHub</a></footer>
  </div>
}
