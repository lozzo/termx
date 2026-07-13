import { Activity, CreditCard, Gauge, Gift, Laptop, LogOut, Moon, Settings, ShieldCheck, Sun } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

type Tab = "overview" | "nodes" | "billing" | "account" | "referrals";
type Theme = "light-gray" | "neutral-dark";
interface Order { id: string; plan_id: string; status: string; created_at: string; }
interface Billing { plan_id: string; valid_until?: string; orders: Order[]; }
interface Profile { account_id: string; user_id: string; email: string; display_name: string; password_configured: boolean; }
interface Node { id: string; name: string; kind: string; online: boolean; revoked: boolean; updated_at: string; }
interface ReferralReward { id: string; order_id: string; kind: string; days: number; created_at: string; }
interface ReferralProgram { code: string; referred_count: number; reward_days: number; rewards: ReferralReward[]; }
interface AuditEvent { id: string; action: string; resource_id: string; occurred_at: string; }
interface Center { profile: Profile; nodes: Node[]; referrals: ReferralProgram; audit: AuditEvent[]; billing: Billing; }

const tabs = [["overview", Gauge, "Overview"], ["nodes", Laptop, "Nodes"], ["billing", CreditCard, "Billing"], ["account", Settings, "Account"], ["referrals", Gift, "Referrals"]] as const;
const csrf = () => document.cookie.split("; ").find((value) => value.startsWith("termx_csrf="))?.split("=")[1] ?? "";
const request = (path: string, body: object, method = "POST") => fetch(path, { method, headers: { "Content-Type": "application/json", "X-TermX-CSRF": csrf() }, body: JSON.stringify(body) });
const when = (value: string) => new Date(value).toLocaleString();
const date = (value: string) => new Date(value).toLocaleDateString();
const savedTheme = (): Theme => localStorage.getItem("termx-wx-theme") === "neutral-dark" ? "neutral-dark" : "light-gray";

export default function AccountPage() {
  const [tab, setTab] = useState<Tab>("overview"), [center, setCenter] = useState<Center | null>(null), [busy, setBusy] = useState(""), [error, setError] = useState("");
  async function load() { const response = await fetch("/api/center", { cache: "no-store" }); if (response.status === 401) { location.href = "/login"; return; } setCenter(await response.json()); }
  useEffect(() => { document.documentElement.dataset.wxTheme = savedTheme(); void load(); }, []);
  async function action(key: string, fn: () => Promise<Response>) { setBusy(key); setError(""); const response = await fn(); if (!response.ok) setError((await response.json().catch(() => ({ error: "Request failed" }))).error); else await load(); setBusy(""); }
  async function logout() { await request("/api/auth/logout", {}); location.href = "/"; }

  if (!center) return <main data-theme-surface className="grid min-h-dvh place-items-center bg-background text-sm text-muted-foreground"><span className="flex items-center gap-3"><i className="size-2 animate-pulse bg-primary" />Loading workspace</span></main>;
  const title = tabs.find(([id]) => id === tab)?.[2];
  return (
    <div data-theme-surface className="min-h-dvh bg-background text-foreground md:grid md:grid-cols-[224px_minmax(0,1fr)]">
      <a className="sr-only focus:not-sr-only focus:fixed focus:left-3 focus:top-3 focus:z-50 focus:bg-primary focus:px-4 focus:py-3 focus:text-primary-foreground" href="#account-content">Skip to content</a>
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-56 flex-col border-r border-line bg-background md:flex">
        <Logo />
        <nav className="flex-1 p-5">{tabs.map(([id, Icon, label]) => <button className={cn("flex h-11 w-full items-center gap-3 border-b border-transparent text-sm text-muted-foreground hover:text-foreground", tab === id && "border-primary text-foreground")} key={id} onClick={() => setTab(id)}><Icon className={cn("size-4", tab === id && "text-primary")} />{label}</button>)}</nav>
        <div className="grid grid-cols-[32px_minmax(0,1fr)_32px] items-center gap-2.5 border-t border-line p-4">
          <span className="grid size-8 place-items-center border border-line text-[10px]">{center.profile.display_name.slice(0, 2).toUpperCase()}</span>
          <div className="min-w-0"><strong className="block truncate text-xs font-medium">{center.profile.display_name}</strong><small className="block truncate text-[9px] text-muted-foreground">{center.profile.email}</small></div>
          <Button aria-label="Sign out" variant="ghost" size="icon" onClick={logout}><LogOut /></Button>
        </div>
      </aside>

      <header className="sticky top-0 z-30 flex h-16 items-center justify-between border-b border-line bg-background px-4 md:hidden"><Logo compact /><ThemePicker compact /></header>
      <main id="account-content" className="min-w-0 pb-28 md:col-start-2 md:pb-16">
        <header className="hidden h-16 items-center justify-between border-b border-line px-8 text-[9px] text-muted-foreground md:flex"><span>TERMX / WEB CONTROLLER</span><div className="flex items-center gap-4"><ThemePicker /><b className="border border-line px-2.5 py-1.5 text-[9px] font-medium text-success">STAGING</b></div></header>
        <div className="mx-auto w-full max-w-[1120px] px-4 py-8 md:px-8 md:py-12">
          <header className="mb-8"><p className="m-0 font-mono text-[9px] text-primary">CLOUD CONTROL</p><h1 className="mt-2 text-4xl font-light md:text-5xl">{title}</h1></header>
          {error && <p className="mb-5 border border-destructive p-3 text-xs text-destructive" role="alert">{error}</p>}
          {tab === "overview" && <Overview value={center} />}
          {tab === "nodes" && <Nodes value={center.nodes} busy={busy} action={action} />}
          {tab === "billing" && <BillingView value={center.billing} busy={busy} action={action} />}
          {tab === "account" && <Account value={center.profile} busy={busy} action={action} />}
          {tab === "referrals" && <Referrals value={center.referrals} />}
        </div>
      </main>
      <nav className="fixed inset-x-0 bottom-0 z-40 grid h-20 grid-cols-5 border-t border-line bg-background md:hidden">{tabs.map(([id, Icon, label]) => <button className={cn("grid place-items-center content-center gap-1 text-[9px] text-muted-foreground", tab === id && "bg-soft text-primary")} key={id} onClick={() => setTab(id)}><Icon className="size-4" /><span>{label}</span></button>)}</nav>
    </div>
  );
}

function Logo({ compact = false }: { compact?: boolean }) { return <a className={cn("flex h-16 items-center gap-3 border-b border-line px-5", compact && "h-auto border-0 p-0")} href="/"><b className="grid size-8 place-items-center bg-primary font-mono text-[11px] font-medium text-primary-foreground">TX</b><span className="grid text-sm font-medium">TermX<small className="text-[9px] font-normal text-muted-foreground">Web Controller</small></span></a>; }
function ThemePicker({ compact = false }: { compact?: boolean }) {
  const [theme, setTheme] = useState<Theme>(savedTheme());
  function choose(next: Theme) { setTheme(next); localStorage.setItem("termx-wx-theme", next); document.documentElement.dataset.wxTheme = next; }
  return <div className="flex border border-line" aria-label="Color theme"><Button className={cn("border-0 border-r border-line", theme === "light-gray" && "bg-soft text-foreground")} variant="ghost" size={compact ? "icon" : "sm"} aria-label="Light theme" aria-pressed={theme === "light-gray"} onClick={() => choose("light-gray")}><Sun />{!compact && <span>Light</span>}</Button><Button className={cn("border-0", theme === "neutral-dark" && "bg-soft text-foreground")} variant="ghost" size={compact ? "icon" : "sm"} aria-label="Dark theme" aria-pressed={theme === "neutral-dark"} onClick={() => choose("neutral-dark")}><Moon />{!compact && <span>Dark</span>}</Button></div>;
}
function PanelTitle({ title, end, icon }: { title: string; end?: string; icon?: ReactNode }) { return <header className="flex h-14 items-center justify-between border-b border-line px-5"><h2 className="text-sm font-medium">{title}</h2>{end ? <b className="text-[9px] font-medium text-success">{end}</b> : icon}</header>; }
function LedgerRow({ label, value, live }: { label: string; value: string; live?: boolean }) { return <div className="grid min-h-14 grid-cols-[120px_1fr] items-center gap-4 border-b border-line px-5 last:border-0"><dt className="text-[10px] text-muted-foreground">{label}</dt><dd className="m-0 flex items-center gap-2 text-xs">{live && <i className="size-1.5 bg-success" />}{value}</dd></div>; }
function Panel({ children, className }: { children: ReactNode; className?: string }) { return <section className={cn("border border-line bg-panel", className)}>{children}</section>; }

function Overview({ value }: { value: Center }) {
  const active = value.nodes.filter((node) => node.online && !node.revoked).length;
  return <div className="grid gap-4 lg:grid-cols-[1.7fr_1fr]">
    <Panel><PanelTitle title="System status" end="OPERATIONAL" /><dl className="m-0"><LedgerRow label="Nodes" value={`${active} online / ${value.nodes.length} registered`} live /><LedgerRow label="Plan" value={value.billing.plan_id === "pro" ? "TermX Pro" : "Managed Free"} /><LedgerRow label="Route" value="Direct + Relay available" /><LedgerRow label="Referral rewards" value={`${value.referrals.reward_days} bonus days`} /></dl><p className="m-0 flex items-center gap-2 border-t border-line p-5 text-[10px] leading-5 text-muted-foreground"><ShieldCheck className="text-primary" />Terminal authorization stays end to end. Cloud services only manage connectivity.</p></Panel>
    <Panel><PanelTitle title="Account" /><div className="p-5"><b className="block text-lg font-normal">{value.profile.display_name}</b><span className="mt-1 block text-xs text-muted-foreground">{value.profile.email}</span></div><dl className="grid grid-cols-2 border-t border-line"><div className="p-5"><dt className="text-[8px] text-muted-foreground">ACCOUNT ID</dt><dd className="mt-2 break-all text-[10px]">{value.profile.account_id}</dd></div><div className="border-l border-line p-5"><dt className="text-[8px] text-muted-foreground">ORDERS</dt><dd className="mt-2 text-lg">{value.billing.orders.length}</dd></div></dl></Panel>
    <Panel className="lg:col-span-2"><PanelTitle title="Recent activity" icon={<Activity className="size-4 text-primary" />} />{value.audit.length ? value.audit.map((event) => <Event key={event.id} title={event.action.replace(".", " ")} detail={event.resource_id} time={when(event.occurred_at)} />) : <Empty text="No activity yet" />}</Panel>
  </div>;
}

function Nodes({ value, busy, action }: { value: Node[]; busy: string; action: (key: string, fn: () => Promise<Response>) => Promise<void>; }) {
  return <Panel><PanelTitle title="Managed nodes" end={`${value.filter((node) => node.online && !node.revoked).length} ONLINE`} /><p className="m-0 border-b border-line px-5 py-4 text-[10px] leading-5 text-muted-foreground">Revoking removes cloud directory access. Terminal grants remain daemon-owned.</p><Table><TableHeader><TableRow><TableHead>NODE</TableHead><TableHead>UPDATED</TableHead><TableHead>STATUS</TableHead><TableHead /></TableRow></TableHeader><TableBody>{value.map((node) => { const status = node.revoked ? "REVOKED" : node.online ? "ONLINE" : "OFFLINE"; return <TableRow key={node.id}><TableCell><span className="flex min-w-40 items-center gap-3"><Laptop className="size-4 text-primary" /><span><b className="block text-xs font-medium">{node.name}</b><small className="text-[9px] text-muted-foreground">{node.kind}</small></span></span></TableCell><TableCell className="whitespace-nowrap text-[10px] text-muted-foreground">{when(node.updated_at)}</TableCell><TableCell><span className={cn("text-[9px] font-medium", status === "ONLINE" ? "text-success" : status === "REVOKED" ? "text-destructive" : "text-muted-foreground")}>{status}</span></TableCell><TableCell className="text-right">{!node.revoked && <Button variant="destructive" size="sm" disabled={busy === node.id} onClick={() => action(node.id, () => request("/api/nodes/revoke", { node_id: node.id }))}>REVOKE</Button>}</TableCell></TableRow>; })}</TableBody></Table></Panel>;
}

function BillingView({ value, busy, action }: { value: Billing; busy: string; action: (key: string, fn: () => Promise<Response>) => Promise<void>; }) {
  async function checkout() { const checkoutResponse = await request("/api/checkout", { plan_id: "pro" }); if (!checkoutResponse.ok) return checkoutResponse; return request("/api/checkout/confirm", { order_id: (await checkoutResponse.json()).id }); }
  return <div className="grid gap-4"><Panel><PanelTitle title="Subscription" /><div className="bg-inverse p-7 text-inverse-foreground"><span className="text-[9px] text-success">CURRENT PLAN</span><h2 className="my-3 text-3xl font-light">{value.plan_id === "pro" ? "TermX Pro" : "Managed Free"}</h2><p className="text-xs text-inverse-foreground/60">{value.valid_until ? `Active until ${date(value.valid_until)}` : "Direct connectivity and managed signaling included."}</p>{value.plan_id !== "pro" && <Button className="mt-6" disabled={busy === "checkout"} onClick={() => action("checkout", checkout)}>{busy === "checkout" ? "PROCESSING" : "TEST PRO CHECKOUT"}</Button>}</div></Panel><Panel><PanelTitle title="Order history" />{value.orders.length ? value.orders.map((order) => <Event key={order.id} title={order.plan_id.toUpperCase()} detail={order.id} time={`${date(order.created_at)} / ${order.status}`} />) : <Empty text="No orders yet" />}</Panel></div>;
}

function Account({ value, busy, action }: { value: Profile; busy: string; action: (key: string, fn: () => Promise<Response>) => Promise<void>; }) {
  const [name, setName] = useState(value.display_name), [currentPassword, setCurrentPassword] = useState(""), [newPassword, setNewPassword] = useState("");
  return <div className="grid gap-4"><Panel><PanelTitle title="Profile" /><form className="grid max-w-2xl gap-5 p-5" onSubmit={(event) => { event.preventDefault(); void action("profile", () => request("/api/profile", { display_name: name }, "PATCH")); }}><Field label="DISPLAY NAME"><Input value={name} onChange={(event) => setName(event.target.value)} /></Field><Field label="EMAIL" hint="Managed by your identity provider."><Input value={value.email} disabled /></Field><div className="flex items-center gap-3 bg-soft p-4 text-[9px]"><ShieldCheck className="text-primary" /><span>ACCOUNT IDENTITY<small className="mt-1 block break-all text-muted-foreground">{value.user_id} / {value.account_id}</small></span></div><Button className="justify-self-start" disabled={busy === "profile"}>{busy === "profile" ? "SAVING" : "SAVE CHANGES"}</Button></form></Panel><Panel><PanelTitle title={value.password_configured ? "Change password" : "Set password"} /><form className="grid max-w-2xl gap-5 p-5" onSubmit={(event) => { event.preventDefault(); void action("password", () => request("/api/password", { current_password: currentPassword, new_password: newPassword })).then(() => { setCurrentPassword(""); setNewPassword(""); }); }}>{value.password_configured && <Field label="CURRENT PASSWORD"><Input required type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} /></Field>}<Field label="NEW PASSWORD" hint="Use at least 10 characters."><Input required minLength={10} type="password" autoComplete="new-password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} /></Field><Button className="justify-self-start" disabled={busy === "password"}>{busy === "password" ? "SAVING" : value.password_configured ? "CHANGE PASSWORD" : "SET PASSWORD"}</Button></form></Panel></div>;
}

function Referrals({ value }: { value: ReferralProgram }) {
  const link = `${window.location.origin}/login?aff=${value.code}`;
  return <div className="grid gap-4"><Panel><PanelTitle title="Invite and earn" end={`${value.reward_days} BONUS DAYS`} /><div className="bg-inverse p-7 text-inverse-foreground"><span className="text-[9px] text-success">YOUR AFF LINK</span><h2 className="my-3 text-3xl font-light">{value.code}</h2><p className="break-all text-xs text-inverse-foreground/60">{link}</p><Button className="mt-6" onClick={() => navigator.clipboard.writeText(link)}>COPY LINK</Button></div><dl className="m-0"><LedgerRow label="Successful referrals" value={`${value.referred_count}`} /><LedgerRow label="Your reward" value="15 days after first payment" /><LedgerRow label="Friend reward" value="7 days after first payment" /></dl></Panel><Panel><PanelTitle title="Reward history" />{value.rewards.length ? value.rewards.map((reward) => <Event key={reward.id} title={reward.kind === "referrer" ? "Referral paid" : "Welcome reward"} detail={reward.order_id} time={`${date(reward.created_at)} / +${reward.days} DAYS`} />) : <Empty text="No rewards yet" />}</Panel></div>;
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) { return <label className="grid gap-2 font-mono text-[9px] text-muted-foreground">{label}{children}{hint && <small className="font-sans text-[10px]">{hint}</small>}</label>; }
function Event({ title, detail, time }: { title: string; detail: string; time: string }) { return <div className="grid min-h-16 grid-cols-[1fr_auto] items-center gap-4 border-b border-line px-5 last:border-0"><span className="min-w-0"><b className="block text-xs font-medium capitalize">{title}</b><small className="block truncate text-[9px] text-muted-foreground">{detail}</small></span><time className="text-[9px] text-muted-foreground">{time}</time></div>; }
function Empty({ text }: { text: string }) { return <p className="m-0 p-10 text-center text-xs text-muted-foreground">{text}</p>; }
