import { useEffect, useState } from "react";
import {
  ArrowRight,
  Check,
  Cloud,
  ExternalLink,
  Folder,
  GitFork,
  History,
  Laptop,
  LockKeyhole,
  Moon,
  Network,
  RadioTower,
  Server,
  ShieldCheck,
  Smartphone,
  Sun,
  Terminal,
} from "lucide-react";
import { Catalog, formatPlanPrice, planPriceNote } from "./catalog";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type Theme = "light-gray" | "neutral-dark";

const coreFeatures = [
  [Terminal, "Persistent terminal pool", "Terminals live in the daemon, independent of the pane or window currently showing them."],
  [History, "Authoritative history", "Reconnect to the same lifecycle, live surface and history instead of rebuilding context from a local scrollback."],
  [Network, "Local, SSH and WebRTC", "One endpoint model reaches local machines, SSH daemons and managed cloud devices without changing terminal identity."],
  [Folder, "Files beside the terminal", "Browse, preview, upload and download files through the same authorized protocol session."],
];

const productLayers = [
  {
    icon: Terminal,
    eyebrow: "OPEN SOURCE / GOLANG",
    title: "TermX Core",
    copy: "The daemon, CLI and TUI form a terminal workspace built around long-lived terminals rather than disposable panes.",
    points: ["No cloud account required", "Local and SSH stay free", "Multi-endpoint workbench"],
    action: { label: "VIEW SOURCE", href: "https://github.com/lozzo/termx", external: true },
  },
  {
    icon: Smartphone,
    eyebrow: "OFFICIAL CLIENT",
    title: "TermX App",
    copy: "Carry the same endpoint list in your pocket. Pair with a daemon, reconnect to running work and move files without exposing a web terminal.",
    points: ["Direct or Relay path shown", "Terminal and file workflows", "Background recovery"],
  },
  {
    icon: Cloud,
    eyebrow: "OFFICIAL MANAGED SERVICE",
    title: "TermX Cloud",
    copy: "The official cloud service for TermX handles device discovery, signaling and managed Relay when changing networks cannot connect directly.",
    points: ["Managed device directory", "Direct P2P coordination", "Single Relay and account controls"],
    action: { label: "EXPLORE PLANS", href: "#plans", external: false },
  },
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
    <main data-theme-surface className="min-w-0 overflow-hidden bg-background text-foreground">
      <header className="sticky top-0 z-40 h-[66px] border-b border-line bg-background/95 backdrop-blur-xl">
        <div className="mx-auto grid h-full w-[min(1360px,calc(100%_-_48px))] grid-cols-[1fr_auto_1fr] items-center max-md:w-[calc(100%_-_28px)] max-md:grid-cols-[1fr_auto]">
          <Brand href="#top" />
          <nav className="flex items-center gap-6 text-[9px] font-semibold text-muted-foreground max-md:hidden" aria-label="Primary navigation">
            <a href="#product">PRODUCT</a><a href="#app">APP</a><a href="#cloud">CLOUD</a><a href="#plans">PLANS</a>
            <a className="flex items-center gap-1" href="https://github.com/lozzo/termx" target="_blank" rel="noreferrer">SOURCE <ExternalLink className="size-2.5" /></a>
          </nav>
          <div className="justify-self-end flex items-center gap-3">
            <div className="flex h-9 border border-line" aria-label="Color theme">
              <button className={cn("grid w-9 place-items-center border-r border-line bg-panel text-muted-foreground max-md:w-11", theme === "light-gray" && "bg-soft text-foreground")} onClick={() => selectTheme("light-gray")} aria-label="Use light theme" title="Light theme"><Sun className="size-3.5" /></button>
              <button className={cn("grid w-9 place-items-center bg-panel text-muted-foreground max-md:w-11", theme === "neutral-dark" && "bg-soft text-foreground")} onClick={() => selectTheme("neutral-dark")} aria-label="Use dark theme" title="Dark theme"><Moon className="size-3.5" /></button>
            </div>
            <a className={cn(buttonVariants({ size: "sm" }), "max-md:hidden")} href="/login">SIGN IN <ArrowRight /></a>
          </div>
        </div>
      </header>

      <section className="min-h-[calc(100dvh-66px)] border-b border-line bg-background pt-20 max-md:min-h-0 max-md:pt-14" id="top">
        <div className={`${wrap} grid grid-cols-[1.35fr_.65fr] gap-x-18 max-lg:grid-cols-2 max-lg:gap-x-10 max-md:grid-cols-1`}>
          <div>
            <Kicker>OPEN-SOURCE TERMINAL MULTIPLEXER</Kicker>
            <h1 className="m-0 text-[74px] font-light leading-[.9] max-md:text-[52px]">TermX</h1>
            <p className="mt-7 text-[46px] font-light leading-[1.08] max-md:mt-5 max-md:text-[34px]">Your terminals<br />outlive the window.</p>
          </div>
          <div className="self-end pb-1 max-md:mt-8">
            <p className="m-0 text-[15px] leading-7 text-muted-foreground max-md:text-sm">A daemon-first terminal workspace for people whose work spans machines, networks and screens. Keep terminals alive in one pool, then observe and operate them from the TUI or official App.</p>
            <div className="mt-7 grid gap-2"><a className={buttonVariants()} href="/login">START WITH TERMX CLOUD <ArrowRight /></a><a className={buttonVariants({ variant: "outline" })} href="https://github.com/lozzo/termx" target="_blank" rel="noreferrer"><GitFork /> OPEN-SOURCE CORE</a></div>
          </div>

          <div className="col-span-full mt-16 border border-b-0 border-line-strong bg-panel max-md:mt-12" aria-label="TermX terminal pool model">
            <header className="flex min-h-11 items-center justify-between border-b border-line px-4 text-[8px] text-muted-foreground"><span>TERMINAL POOL / BUILD-MACHINE</span><strong className="flex items-center gap-2 font-semibold text-success"><i className="size-1.5 rounded-full bg-success" /> DAEMON ONLINE</strong></header>
            <div className="grid min-h-60 grid-cols-[310px_minmax(300px,1fr)_270px] max-lg:grid-cols-[250px_1fr] max-md:grid-cols-1">
              <div className="border-r border-line bg-soft max-md:border-r-0">
                <TerminalRow active name="api-dev" meta="RUNNING / 02:14:37" status="LIVE" />
                <TerminalRow name="agent-refactor" meta="RUNNING / 08:41:02" status="LIVE" />
                <TerminalRow name="release-check" meta="EXITED / HISTORY KEPT" status="137" />
              </div>
              <div className="flex min-h-60 flex-col justify-center px-11 py-9 max-md:border-t max-md:border-line max-md:px-6">
                <p className="m-0 text-[8px] font-semibold text-primary">THE TERMINAL IS THE WORK.</p>
                <strong className="mt-5 max-w-md text-2xl font-light leading-tight">Windows, panes and devices are only views.</strong>
                <span className="mt-4 max-w-lg text-[11px] leading-5 text-muted-foreground">Detach the TUI. Close the App. Change the layout. The daemon keeps lifecycle and history where the terminal actually runs.</span>
              </div>
              <div className="border-l border-line p-6 max-lg:col-span-full max-lg:grid max-lg:grid-cols-[auto_1fr_1fr] max-lg:items-center max-lg:border-l-0 max-lg:border-t max-md:col-auto max-md:block">
                <p className="m-0 mb-4 text-[7px] text-muted-foreground">AVAILABLE FROM</p>
                <Observer icon={<Laptop />} title="Desktop TUI" detail="LOCAL / SSH / CLOUD" /><Observer icon={<Smartphone />} title="Official App" detail="DIRECT / RELAY" />
              </div>
            </div>
            <footer className="grid min-h-12 grid-cols-[auto_auto_auto_1fr] items-center gap-6 border-t border-line px-4 text-[7px] text-muted-foreground max-md:min-h-24 max-md:grid-cols-2 max-md:gap-3"><span>LOCAL UNIX SOCKET</span><span>SSH DAEMON</span><span>MANAGED WEBRTC</span><strong className="justify-self-end text-foreground max-md:justify-self-start">ONE ENDPOINT MODEL</strong></footer>
          </div>
        </div>
      </section>

      <section className="border-b border-line bg-panel py-28 max-md:py-20" id="product">
        <div className={wrap}>
          <SectionHeading kicker="ONE PRODUCT / THREE LAYERS">Use the open core alone.<br />Add the App and Cloud when you need reachability.</SectionHeading>
          <div className="mt-16 grid grid-cols-3 border-l border-t border-line-strong max-md:mt-11 max-md:grid-cols-1">
            {productLayers.map((product) => {
              const Icon = product.icon;
              return <article className="flex min-h-[430px] flex-col border-b border-r border-line-strong p-8 even:bg-soft max-lg:p-6" id={product.title === "TermX App" ? "app" : product.title === "TermX Cloud" ? "cloud" : undefined} key={product.title}>
                <header className="flex items-center gap-2.5 text-[8px] font-semibold text-primary"><Icon className="size-4" /><span>{product.eyebrow}</span></header>
                <h3 className="mt-16 text-[28px] font-light">{product.title}</h3><p className="mt-4 min-h-20 text-[11px] leading-5 text-muted-foreground">{product.copy}</p>
                <ul className="mt-6 flex-1 space-y-3 border-t border-line pt-6 text-[9px] text-muted-foreground">{product.points.map((point) => <li className="flex gap-2" key={point}><Check className="size-3 text-success" /> {point}</li>)}</ul>
                {product.action && <a className="flex min-h-11 items-center justify-between border-t border-line text-[9px] font-semibold" href={product.action.href} target={product.action.external ? "_blank" : undefined} rel={product.action.external ? "noreferrer" : undefined}>{product.action.label} {product.action.external ? <ExternalLink className="size-3" /> : <ArrowRight className="size-3" />}</a>}
              </article>;
            })}
          </div>
          <p className="m-0 flex min-h-16 items-center gap-3 border-x border-b border-line px-5 text-[10px] leading-5 text-muted-foreground"><ShieldCheck className="size-4 shrink-0 text-primary" /> TermX Cloud and the official mobile App are proprietary TermX products. The open-source Go core remains fully usable with local and SSH endpoints without a cloud account.</p>
        </div>
      </section>

      <section className="border-b border-line bg-background py-28 max-md:py-20">
        <div className={wrap}>
          <SectionHeading kicker="BUILT FOR LONG-RUNNING WORK">Not another arrangement<br />of disposable terminal panes.</SectionHeading>
          <div className="mt-16 border-t border-line-strong max-md:mt-11">
            {coreFeatures.map(([FeatureIcon, title, copy], index) => <article className="grid min-h-32 grid-cols-[45px_45px_240px_1fr] items-center gap-5 border-b border-line max-md:min-h-48 max-md:grid-cols-[28px_32px_1fr] max-md:gap-3 max-md:py-6" key={title as string}><span className="text-[8px] text-primary">0{index + 1}</span><FeatureIcon className="size-4 text-primary" /><h3 className="text-lg font-normal">{title as string}</h3><p className="m-0 max-w-2xl text-xs leading-5 text-muted-foreground max-md:col-start-3">{copy as string}</p></article>)}
          </div>
        </div>
      </section>

      <section className="border-b border-line bg-panel py-28 max-md:py-20" id="connectivity">
        <div className={wrap}>
          <SectionHeading kicker="OFFICIAL CLOUD CONNECTIVITY" copy="TermX Cloud gets the client and daemon to the same encrypted session. It does not receive terminal capability grants, commands, history, file metadata or terminal payload.">Direct when possible.<br />Relay when necessary.</SectionHeading>
          <div className="mt-16 grid min-h-72 grid-cols-[260px_1fr_260px] border border-line-strong max-lg:grid-cols-[210px_1fr_210px] max-md:mt-11 max-md:grid-cols-1" aria-label="TermX Cloud connection model">
            <RouteNode icon={<Smartphone />} label="CLIENT" title="TERMX APP" status="IDENTITY VERIFIED" />
            <div className="grid grid-rows-[1fr_55px_55px] border-l border-line max-md:min-h-52 max-md:border-l-0 max-md:border-t">
              <div className="flex flex-col items-center justify-center text-muted-foreground"><Cloud className="mb-2 size-4 text-primary" /><span className="text-[8px] font-semibold">TERMX CLOUD</span><small className="mt-1 text-[7px]">DIRECTORY / AUTH / SIGNALING</small></div>
              <RouteLine active label="DIRECT P2P" value="42 MS" /><RouteLine label="SINGLE RELAY" value="AVAILABLE" />
            </div>
            <RouteNode icon={<Server />} label="OWNER" title="TERMX DAEMON" status="CAPABILITY VERIFIED" last />
          </div>
          <div className="grid grid-cols-3 border-x border-b border-line max-md:grid-cols-1"><RouteFact icon={<LockKeyhole />} text="DTLS end-to-end encrypted" /><RouteFact icon={<RadioTower />} text="Actual direct or Relay path shown" /><RouteFact icon={<ShieldCheck />} text="Terminal truth remains on the daemon" /></div>
        </div>
      </section>

      <section className="border-b border-line bg-soft py-28 max-md:py-20" id="plans">
        <div className={wrap}>
          <SectionHeading kicker="TERMX CLOUD PLANS" copy="Local, SSH, multi-endpoint workspaces and terminal capability remain part of the open core. Plans fund official discovery, Relay capacity and account services.">Pay for managed infrastructure,<br />not terminal features.</SectionHeading>
          <div className="mt-16 grid min-h-[470px] grid-cols-3 border-l border-t border-line-strong max-md:mt-11 max-md:min-h-0 max-md:grid-cols-1" aria-live="polite">
            {catalog?.plans.map((plan) => <article className={cn("flex flex-col border-b border-r border-line-strong bg-panel p-7", plan.featured && "shadow-[inset_0_3px_0_var(--primary)]")} key={plan.id}>
              <header className="flex justify-between gap-3 text-[8px] text-muted-foreground"><span>{plan.eyebrow}</span>{plan.featured && <b className="text-primary">RECOMMENDED</b>}</header><h3 className="mt-7 text-[27px] font-light">{plan.name}</h3><p className="mt-3 min-h-16 text-[10px] leading-4 text-muted-foreground max-md:min-h-0">{plan.description}</p>
              <div className="mt-4 flex min-h-20 items-baseline gap-2 border-y border-line py-5"><strong className="text-[27px] font-normal">{formatPlanPrice(plan, catalog.currency)}</strong><small className="text-[8px] text-muted-foreground">{planPriceNote(plan)}</small></div>
              <ul className="my-6 flex-1 space-y-3 text-[9px] text-muted-foreground">{plan.features.map((feature) => <li className="flex gap-2 leading-4" key={feature}><Check className="size-3 shrink-0 text-success" /> {feature}</li>)}</ul>
              <a className={buttonVariants({ variant: plan.featured ? "default" : "outline" })} href={plan.cta.href}>{plan.cta.label.toUpperCase()} <ArrowRight /></a>
            </article>)}
            {!catalog && <p className="col-span-full grid min-h-72 place-items-center border-b border-r border-line-strong text-[9px] text-muted-foreground">LOADING CLOUD PLANS</p>}
          </div>
        </div>
      </section>

      <section className="bg-inverse py-24 text-inverse-foreground max-md:py-20">
        <div className={`${wrap} grid grid-cols-[.6fr_1.4fr_.7fr] items-end gap-12 max-md:grid-cols-1 max-md:items-start max-md:gap-6`}><p className="self-start text-[8px] opacity-55">OPEN CORE / OFFICIAL CLOUD</p><h2 className="text-[38px] font-light leading-tight max-md:text-3xl">Keep the terminal.<br />Change where you see it.</h2><div className="grid gap-2"><a className={buttonVariants()} href="/login">CREATE ACCOUNT <ArrowRight /></a><a className={cn(buttonVariants({ variant: "outline" }), "border-white/30 bg-transparent text-white")} href="https://github.com/lozzo/termx" target="_blank" rel="noreferrer">VIEW GITHUB <ExternalLink /></a></div></div>
      </section>

      <footer className="min-h-20 bg-inverse text-inverse-foreground"><div className={`${wrap} grid min-h-20 grid-cols-[1fr_auto_1fr] items-center border-t border-white/15 max-md:grid-cols-[1fr_auto]`}><Brand href="#top" inverse /><a className="flex items-center gap-2 text-[8px] opacity-50 hover:opacity-100 max-md:hidden" href="https://github.com/lozzo/termx" target="_blank" rel="noreferrer"><GitFork className="size-3" /> OPEN-SOURCE GO CORE</a><span className="justify-self-end text-[8px] opacity-50">TERMX CLOUD / OFFICIAL SERVICE</span></div></footer>
    </main>
  );
}

const wrap = "mx-auto w-[min(1200px,calc(100%_-_64px))] max-md:w-[calc(100%_-_28px)]";
function Brand({ href, inverse = false }: { href: string; inverse?: boolean }) { return <a className="flex items-center gap-2.5 justify-self-start" href={href} aria-label="TermX home"><b className={cn("grid size-8 place-items-center bg-foreground text-[10px] font-bold text-background", inverse && "bg-inverse-foreground text-inverse")}>TX</b><span className="grid text-[13px] font-semibold leading-tight">TERMX<small className="text-[7px] font-normal text-muted-foreground">TERMINAL WORKSPACE</small></span></a>; }
function Kicker({ children }: { children: React.ReactNode }) { return <p className="mb-5 flex items-center gap-2 text-[9px] font-semibold text-muted-foreground"><i className="h-px w-5 bg-primary" />{children}</p>; }
function SectionHeading({ kicker, copy, children }: { kicker: string; copy?: string; children: React.ReactNode }) { return <header className="max-w-[820px]"><Kicker>{kicker}</Kicker><h2 className="text-[45px] font-light leading-tight max-md:text-[33px]">{children}</h2>{copy && <p className="mt-6 max-w-[690px] text-sm leading-6 text-muted-foreground max-md:text-[13px]">{copy}</p>}</header>; }
function TerminalRow({ active, name, meta, status }: { active?: boolean; name: string; meta: string; status: string }) { return <div className={cn("grid min-h-20 grid-cols-[30px_1fr_auto] items-center gap-3 border-b border-line px-4 text-muted-foreground last:border-0", active && "bg-panel text-foreground shadow-[inset_3px_0_0_var(--primary)]")}><Terminal className="size-4 text-primary" /><span><strong className="block text-[11px] font-medium">{name}</strong><small className="mt-1 block text-[7px] text-muted-foreground">{meta}</small></span><em className="text-[7px] not-italic text-success">{status}</em></div>; }
function Observer({ icon, title, detail }: { icon: React.ReactNode; title: string; detail: string }) { return <div className="flex min-h-16 items-center gap-3 border-t border-line max-lg:border-l max-lg:border-t-0 max-lg:pl-5 max-md:border-l-0 max-md:border-t max-md:pl-0"><span className="text-primary">{icon}</span><span><strong className="block text-[10px] font-medium">{title}</strong><small className="mt-1 block text-[7px] text-muted-foreground">{detail}</small></span></div>; }
function RouteNode({ icon, label, title, status, last }: { icon: React.ReactNode; label: string; title: string; status: string; last?: boolean }) { return <div className={cn("flex min-h-44 flex-col justify-center bg-soft p-8", last && "border-l border-line bg-panel max-md:border-l-0 max-md:border-t")}><span className="mb-8 text-primary">{icon}</span><small className="text-[7px] text-muted-foreground">{label}</small><strong className="mt-2 text-[13px] font-semibold">{title}</strong><span className="mt-2 text-[7px] text-success">{status}</span></div>; }
function RouteLine({ active, label, value }: { active?: boolean; label: string; value: string }) { return <div className={cn("grid grid-cols-[auto_1fr_auto] items-center gap-3 border-t border-line px-4 text-[7px] text-muted-foreground", active && "text-success")}><b className="text-[8px] font-semibold">{label}</b><i className={cn("h-px bg-line-strong", active && "bg-success")} /><strong className="text-[8px]">{value}</strong></div>; }
function RouteFact({ icon, text }: { icon: React.ReactNode; text: string }) { return <p className="m-0 flex min-h-14 items-center gap-2 border-r border-line px-4 text-[9px] text-muted-foreground last:border-0 max-md:border-b max-md:border-r-0 max-md:last:border-b-0"><span className="text-success">{icon}</span>{text}</p>; }
