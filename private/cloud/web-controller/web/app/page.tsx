import Image from 'next/image'
import { ArrowRight, Check, Cloud, LockKeyhole, Network, RadioTower, ShieldCheck, Users } from 'lucide-react'
import { formatPlanPrice, loadCatalog, planPriceNote } from '../lib/catalog'

export const dynamic = 'force-dynamic'

const productFacts = [
  ['Direct first', 'P2P remains the fastest path when the network allows it.'],
  ['Relay by intent', 'Single Relay is explicit, observable and quota-bound.'],
  ['Terminal truth stays home', 'History, input and capability checks remain on your daemon.'],
]

export default async function HomePage() {
  const catalog = await loadCatalog()
  return (
    <main>
      <header className="site-header">
        <a className="brand" href="#top" aria-label="TermX home">TermX</a>
        <nav aria-label="Primary navigation">
          <a href="#product">Product</a>
          <a href="#plans">Plans</a>
          <a href="#security">Security</a>
        </nav>
        <a className="header-action" href="#access">Get access <ArrowRight size={16} /></a>
      </header>

      <section className="hero" id="top">
        <Image alt="TermX machine workspace showing managed endpoints and connection paths" fill priority sizes="100vw" src="/product-workspace.png" />
        <div className="hero-shade" />
        <div className="hero-content">
          <p className="kicker"><span /> Managed terminal connectivity</p>
          <h1>TermX</h1>
          <p className="hero-copy">Your terminals, reachable anywhere. Direct P2P when possible, managed Relay when networks disagree, and end-to-end authorization on every path.</p>
          <div className="hero-actions">
            <a className="button primary" href="#plans">Explore plans <ArrowRight size={18} /></a>
            <a className="button quiet" href="#product">See how it works</a>
          </div>
        </div>
        <div className="path-proof" aria-label="Connection path example">
          <span><RadioTower size={16} /> Public staging</span>
          <strong>Connected</strong>
          <span>Single Relay</span>
          <span>62 ms</span>
        </div>
      </section>

      <section className="facts" id="product">
        {productFacts.map(([title, copy], index) => (
          <article key={title}>
            <span className="fact-number">0{index + 1}</span>
            <h2>{title}</h2>
            <p>{copy}</p>
          </article>
        ))}
      </section>

      <section className="product-section" id="security">
        <div className="section-heading">
          <p className="kicker dark"><span /> One connection model</p>
          <h2>Cloud reachability without cloud-owned terminals.</h2>
          <p>Hub and Relay coordinate encrypted connectivity. Your daemon remains the owner of terminal lifecycle, history and capability authorization.</p>
        </div>
        <div className="system-map" aria-label="TermX security boundaries">
          <div className="system-node"><Cloud /><span>Managed cloud</span><small>Directory · Signaling · Relay</small></div>
          <div className="system-link"><span>DTLS encrypted</span></div>
          <div className="system-node accent"><LockKeyhole /><span>Your daemon</span><small>Capability · Terminal · History</small></div>
        </div>
        <div className="security-notes">
          <span><ShieldCheck size={18} /> Capability grants stay inside the encrypted DataChannel</span>
          <span><Network size={18} /> Every connection reports its actual direct or Relay path</span>
        </div>
      </section>

      <section className="plans-section" id="plans">
        <div className="section-heading plans-heading">
          <p className="kicker dark"><span /> Plans</p>
          <h2>Pay for managed infrastructure, not your terminals.</h2>
          <p>Local connections, SSH and terminal capabilities remain yours. Plans add official discovery, Relay capacity and organization controls.</p>
        </div>
        <div className="plan-grid">
          {catalog.plans.map((plan) => (
            <article className={`plan ${plan.featured ? 'featured' : ''}`} key={plan.id}>
              <div>
                <p className="plan-eyebrow">{plan.eyebrow}</p>
                <h3>{plan.name}</h3>
                <p className="plan-description">{plan.description}</p>
              </div>
              <div className="price">
                <strong>{formatPlanPrice(plan, catalog.currency)}</strong>
                <span>{planPriceNote(plan)}</span>
              </div>
              <ul>
                {plan.features.map((feature) => <li key={feature}><Check size={16} /> {feature}</li>)}
              </ul>
              <a className={`button ${plan.featured ? 'primary' : 'outline'}`} href={plan.cta.href}>{plan.cta.label} <ArrowRight size={17} /></a>
            </article>
          ))}
        </div>
      </section>

      <section className="access-section" id="access">
        <div><Users size={24} /><span>Private preview</span></div>
        <h2>Bring the terminal. We will handle the difficult networks.</h2>
        <a className="button primary light" href="mailto:hello@termx.dev?subject=TermX%20access">Request access <ArrowRight size={18} /></a>
      </section>

      <footer><a className="brand" href="#top">TermX</a><p>Terminal truth stays with the machine that owns it.</p><span>Private preview</span></footer>
    </main>
  )
}
