import { useEffect, useState } from "react";
import {
  ArrowRight,
  Check,
  Cloud,
  ExternalLink,
  GitFork,
  Laptop,
  LockKeyhole,
  Moon,
  RadioTower,
  Server,
  ShieldCheck,
  Sun,
  Zap,
} from "lucide-react";
import { Catalog, formatPlanPrice, planPriceNote } from "./catalog";

type Theme = "light-gray" | "neutral-dark";

const productFacts = [
  ["01", "Direct first", "P2P remains the preferred data path whenever the network allows it."],
  ["02", "Relay when required", "A single managed Relay handles difficult networks with explicit path reporting."],
  ["03", "Truth stays home", "Terminal lifecycle, history and capability checks remain on the owning daemon."],
];

export default function HomePage() {
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [theme, setTheme] = useState<Theme>(() =>
    localStorage.getItem("termx-wx-theme") === "neutral-dark" ? "neutral-dark" : "light-gray",
  );

  useEffect(() => {
    fetch("/api/catalog", { cache: "no-store" })
      .then((response) => {
        if (!response.ok) throw new Error("catalog unavailable");
        return response.json();
      })
      .then(setCatalog)
      .catch(() => setCatalog(null));
  }, []);

  function selectTheme(next: Theme) {
    setTheme(next);
    document.documentElement.dataset.wxTheme = next;
    localStorage.setItem("termx-wx-theme", next);
  }

  return (
    <main id="lx-root">
      <header className="lx-header">
        <div className="lx-header-inner">
          <a className="lx-logo" href="#top" aria-label="TermX home">
            <b>TX</b>
            <span>TERMX<small>MANAGED EDGE</small></span>
          </a>
          <nav aria-label="Primary navigation">
            <a href="#network">NETWORK</a>
            <a href="#security">SECURITY</a>
            <a href="#plans">PLANS</a>
            <a className="lx-source-nav" href="https://github.com/lozzo/termx" target="_blank" rel="noreferrer">SOURCE <ExternalLink /></a>
          </nav>
          <div className="lx-header-tools">
            <div className="lx-theme" aria-label="Color theme">
              <button className={theme === "light-gray" ? "selected" : ""} onClick={() => selectTheme("light-gray")} aria-label="Use light theme" title="Light theme"><Sun /></button>
              <button className={theme === "neutral-dark" ? "selected" : ""} onClick={() => selectTheme("neutral-dark")} aria-label="Use dark theme" title="Dark theme"><Moon /></button>
            </div>
            <a className="lx-sign-in" href="/login">SIGN IN <ArrowRight /></a>
          </div>
        </div>
      </header>

      <section className="lx-hero" id="top">
        <div className="lx-wrap">
          <div className="lx-hero-copy">
            <p className="lx-overline"><i /> MANAGED TERMINAL CONNECTIVITY</p>
            <h1>TermX</h1>
            <p className="lx-lead">One terminal network for every machine.</p>
            <p className="lx-summary">Connect locally, establish direct P2P remotely, or use a managed Relay when the network gets in the way. The terminal itself remains owned by your daemon.</p>
            <div className="lx-actions">
              <a className="lx-action primary" href="/login">GET STARTED <ArrowRight /></a>
              <a className="lx-action" href="#network">VIEW CONNECTION MODEL</a>
            </div>
          </div>

          <figure className="lx-network" id="network" aria-label="TermX managed connection topology">
            <figcaption><span>CONNECTION / TMX-7A2F</span><strong><i /> ESTABLISHED</strong><time>14:22:08 UTC</time></figcaption>
            <div className="lx-topology">
              <div className="lx-endpoint">
                <span className="lx-node-icon"><Laptop /></span>
                <small>CLIENT</small>
                <strong>MACBOOK-PRO</strong>
                <p>IDENTITY VERIFIED</p>
              </div>
              <div className="lx-routes">
                <div className="lx-coordination"><Cloud /><span>HUB COORDINATION</span><small>AUTH + SIGNALING</small></div>
                <div className="lx-route active"><span>DIRECT P2P</span><i /><strong>42 MS</strong></div>
                <div className="lx-route"><span>SINGLE RELAY</span><i /><strong>STANDBY</strong></div>
              </div>
              <div className="lx-endpoint target">
                <span className="lx-node-icon"><Server /></span>
                <small>DAEMON</small>
                <strong>BUILD-SERVER-01</strong>
                <p>3 TERMINALS ONLINE</p>
              </div>
            </div>
            <dl className="lx-telemetry">
              <div><dt>ACTUAL PATH</dt><dd>DIRECT / P2P</dd></div>
              <div><dt>ENCRYPTION</dt><dd>DTLS / E2E</dd></div>
              <div><dt>TRANSPORT</dt><dd>WEBRTC</dd></div>
              <div><dt>CLOUD PAYLOAD</dt><dd>ENCRYPTED BYTES</dd></div>
            </dl>
          </figure>
        </div>
      </section>

      <section className="lx-facts" aria-label="Product principles">
        {productFacts.map(([number, title, copy]) => (
          <article key={number}>
            <span>{number}</span>
            <div><h2>{title}</h2><p>{copy}</p></div>
          </article>
        ))}
      </section>

      <section className="lx-ownership" id="source" aria-label="TermX open-source and cloud service boundary">
        <div className="lx-wrap">
          <header>
            <p className="lx-overline"><i /> PRODUCT BOUNDARY</p>
            <h2>Open core.<br />Official cloud.</h2>
          </header>
          <a className="lx-product-line" href="https://github.com/lozzo/termx" target="_blank" rel="noreferrer">
            <GitFork />
            <span><small>OPEN SOURCE / GOLANG</small><strong>TermX</strong><p>The terminal multiplexer and its Go core are available on GitHub.</p></span>
            <ExternalLink />
          </a>
          <div className="lx-product-line">
            <Cloud />
            <span><small>OFFICIAL MANAGED SERVICE</small><strong>TermX Cloud</strong><p>TermX Cloud is the official cloud service for TermX. The cloud services and official mobile app are proprietary and are not part of the open-source repository.</p></span>
          </div>
        </div>
      </section>

      <section className="lx-section lx-security" id="security">
        <div className="lx-wrap">
          <header className="lx-section-heading">
            <p className="lx-overline"><i /> SECURITY BOUNDARY</p>
            <h2>Cloud reachability.<br />Daemon-owned terminals.</h2>
            <p>The managed plane coordinates who can reach a machine. It does not become the owner of terminal capability, history, input or output.</p>
          </header>
          <div className="lx-boundary">
            <article>
              <span><Cloud /> MANAGED CLOUD</span>
              <h3>Coordinates the path</h3>
              <ul><li>Device directory and presence</li><li>Identity admission and signaling</li><li>Relay lease and usage accounting</li></ul>
            </article>
            <div className="lx-channel"><LockKeyhole /><span>DTLS DATA CHANNEL</span><small>END-TO-END ENCRYPTED</small></div>
            <article>
              <span><ShieldCheck /> OWNING DAEMON</span>
              <h3>Authorizes the terminal</h3>
              <ul><li>Capability grant verification</li><li>Terminal lifecycle and input</li><li>Authoritative history truth</li></ul>
            </article>
          </div>
          <div className="lx-proof">
            <p><Zap /> The route is selected before terminal capability is evaluated.</p>
            <p><RadioTower /> Every session reports its actual local, direct or Relay path.</p>
          </div>
        </div>
      </section>

      <section className="lx-section lx-plans" id="plans">
        <div className="lx-wrap">
          <header className="lx-section-heading">
            <p className="lx-overline"><i /> MANAGED PLANS</p>
            <h2>Pay for the managed edge,<br />not your terminals.</h2>
            <p>Local connections, SSH and terminal capabilities remain yours. Plans add official discovery, managed Relay capacity and account controls.</p>
          </header>
          <div className="lx-plan-grid" aria-live="polite">
            {catalog?.plans.map((plan) => (
              <article className={plan.featured ? "featured" : ""} key={plan.id}>
                <header><p>{plan.eyebrow}</p>{plan.featured && <span>RECOMMENDED</span>}</header>
                <h3>{plan.name}</h3>
                <p className="lx-plan-description">{plan.description}</p>
                <div className="lx-price"><strong>{formatPlanPrice(plan, catalog.currency)}</strong><span>{planPriceNote(plan)}</span></div>
                <ul>{plan.features.map((feature) => <li key={feature}><Check /> {feature}</li>)}</ul>
                <a className={`lx-action ${plan.featured ? "primary" : ""}`} href={plan.cta.href}>{plan.cta.label.toUpperCase()} <ArrowRight /></a>
              </article>
            ))}
            {!catalog && <p className="lx-catalog-status">LOADING PLAN CATALOG</p>}
          </div>
        </div>
      </section>

      <section className="lx-access">
        <div className="lx-wrap">
          <p>PRIVATE PREVIEW / SINGLE REGION</p>
          <h2>Bring the terminal.<br />We handle the difficult networks.</h2>
          <a className="lx-action primary" href="mailto:hello@termx.dev?subject=TermX%20access">REQUEST ACCESS <ArrowRight /></a>
        </div>
      </section>

      <footer className="lx-footer">
        <div className="lx-wrap"><a className="lx-logo" href="#top"><b>TX</b><span>TERMX</span></a><a className="lx-footer-source" href="https://github.com/lozzo/termx" target="_blank" rel="noreferrer"><GitFork /> OPEN-SOURCE GO CORE</a><span>TERMX CLOUD / OFFICIAL SERVICE</span></div>
      </footer>
    </main>
  );
}
