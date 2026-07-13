"use client";

import {
  Activity,
  CreditCard,
  Gauge,
  Laptop,
  LogOut,
  Settings,
  ShieldCheck,
  Gift,
} from "lucide-react";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";

type Tab = "overview" | "nodes" | "billing" | "account" | "referrals";
type WxTheme = "light-gray" | "neutral-dark";
interface Order {
  id: string;
  plan_id: string;
  status: string;
  created_at: string;
}
interface Billing {
  plan_id: string;
  valid_until?: string;
  orders: Order[];
}
interface Profile {
  account_id: string;
  user_id: string;
  email: string;
  display_name: string;
  password_configured: boolean;
}
interface Node {
  id: string;
  name: string;
  kind: string;
  online: boolean;
  revoked: boolean;
  updated_at: string;
}
interface ReferralReward {
  id: string;
  order_id: string;
  kind: string;
  days: number;
  created_at: string;
}
interface ReferralProgram {
  code: string;
  referred_count: number;
  reward_days: number;
  rewards: ReferralReward[];
}
interface AuditEvent {
  id: string;
  action: string;
  resource_id: string;
  occurred_at: string;
}
interface Center {
  profile: Profile;
  nodes: Node[];
  referrals: ReferralProgram;
  audit: AuditEvent[];
  billing: Billing;
}
const tabs = [
  ["overview", Gauge, "Overview"],
  ["nodes", Laptop, "Nodes"],
  ["billing", CreditCard, "Billing"],
  ["account", Settings, "Account"],
  ["referrals", Gift, "Referrals"],
] as const;
const csrf = () =>
  document.cookie
    .split("; ")
    .find((v) => v.startsWith("termx_csrf="))
    ?.split("=")[1] ?? "";
const request = (path: string, body: object, method = "POST") =>
  fetch(path, {
    method,
    headers: { "Content-Type": "application/json", "X-TermX-CSRF": csrf() },
    body: JSON.stringify(body),
  });
const when = (v: string) => new Date(v).toLocaleString();
const date = (v: string) => new Date(v).toLocaleDateString();
const savedTheme = (): WxTheme =>
  localStorage.getItem("termx-wx-theme") === "neutral-dark"
    ? "neutral-dark"
    : "light-gray";

export default function AccountPage() {
  const [tab, setTab] = useState<Tab>("overview"),
    [center, setCenter] = useState<Center | null>(null),
    [busy, setBusy] = useState(""),
    [error, setError] = useState("");
  async function load() {
    const r = await fetch("/api/center", { cache: "no-store" });
    if (r.status === 401) {
      location.href = "/login";
      return;
    }
    setCenter(await r.json());
  }
  useEffect(() => {
    const saved = savedTheme();
    document.documentElement.dataset.wxTheme = saved;
    void load();
  }, []);
  async function action(key: string, fn: () => Promise<Response>) {
    setBusy(key);
    setError("");
    const r = await fn();
    if (!r.ok)
      setError(
        (await r.json().catch(() => ({ error: "Request failed" }))).error,
      );
    else await load();
    setBusy("");
  }
  async function logout() {
    await request("/api/auth/logout", {});
    location.href = "/";
  }
  if (!center)
    return (
      <main id="wx-root" className="wx-loading">
        <span />
        Loading workspace
      </main>
    );
  const title = tabs.find(([id]) => id === tab)?.[2];
  return (
    <div id="wx-root" className="wx-shell">
      <a className="wx-skip" href="#wx-content">
        Skip to content
      </a>
      <aside className="wx-rail">
        <Logo />
        <nav>
          {tabs.map(([id, Icon, label]) => (
            <button
              key={id}
              className={tab === id ? "selected" : ""}
              onClick={() => setTab(id)}
            >
              <Icon />
              <span>{label}</span>
            </button>
          ))}
        </nav>
        <div className="wx-user">
          <span>{center.profile.display_name.slice(0, 2).toUpperCase()}</span>
          <div>
            <strong>{center.profile.display_name}</strong>
            <small>{center.profile.email}</small>
          </div>
          <button aria-label="Sign out" onClick={logout}>
            <LogOut />
          </button>
        </div>
      </aside>
      <header className="wx-mobile-head">
        <Logo />
        <ThemePicker compact />
      </header>
      <main id="wx-content" className="wx-main">
        <header className="wx-top">
          <span>TERMX / WEB CONTROLLER</span>
          <div className="wx-top-actions">
            <ThemePicker />
            <b>STAGING</b>
          </div>
        </header>
        <div className="wx-page">
          <header className="wx-title">
            <p>CLOUD CONTROL</p>
            <h1>{title}</h1>
          </header>
          {error && (
            <p className="wx-error" role="alert">
              {error}
            </p>
          )}
          {tab === "overview" && <Overview value={center} />}{" "}
          {tab === "nodes" && (
            <Nodes value={center.nodes} busy={busy} action={action} />
          )}{" "}
          {tab === "billing" && (
            <BillingView value={center.billing} busy={busy} action={action} />
          )}{" "}
          {tab === "account" && (
            <Account value={center.profile} busy={busy} action={action} />
          )}{" "}
          {tab === "referrals" && (
            <Referrals value={center.referrals} />
          )}
        </div>
      </main>
      <nav className="wx-mobile-nav">
        {tabs.map(([id, Icon, label]) => (
          <button
            key={id}
            className={tab === id ? "selected" : ""}
            onClick={() => setTab(id)}
          >
            <Icon />
            <span>{label}</span>
          </button>
        ))}
      </nav>
    </div>
  );
}
function Logo() {
  return (
    <a className="wx-logo" href="/">
      <b>TX</b>
      <span>
        TermX<small>Web Controller</small>
      </span>
    </a>
  );
}
function ThemePicker({ compact = false }: { compact?: boolean }) {
  const [theme, setTheme] = useState<WxTheme>("light-gray");
  useEffect(() => {
    const saved = savedTheme();
    setTheme(saved);
    document.documentElement.dataset.wxTheme = saved;
  }, []);
  function choose(next: WxTheme) {
    setTheme(next);
    localStorage.setItem("termx-wx-theme", next);
    document.documentElement.dataset.wxTheme = next;
  }
  const options: [WxTheme, string][] = [
    ["light-gray", "Light"],
    ["neutral-dark", "Dark"],
  ];
  return (
    <div
      className={`wx-themes${compact ? " compact" : ""}`}
      aria-label="Color theme"
    >
      {options.map(([id, label]) => (
        <button
          key={id}
          type="button"
          aria-label={`${label} theme`}
          aria-pressed={theme === id}
          className={theme === id ? "selected" : ""}
          onClick={() => choose(id)}
        >
          <i className={`wx-swatch ${id}`} />
          <span>{label}</span>
        </button>
      ))}
    </div>
  );
}
function Overview({ value }: { value: Center }) {
  const active = value.nodes.filter((n) => n.online && !n.revoked).length,
    rewards = value.referrals.reward_days;
  return (
    <div className="wx-overview">
      <section>
        <Section title="System status" end="Operational" />
        <dl className="wx-ledger">
          <Row
            label="Nodes"
            value={`${active} online / ${value.nodes.length} registered`}
            live
          />
          <Row
            label="Plan"
            value={
              value.billing.plan_id === "pro" ? "TermX Pro" : "Managed Free"
            }
          />
          <Row label="Route" value="Direct + Relay available" />
          <Row label="Referral rewards" value={`${rewards} bonus days`} />
        </dl>
        <p className="wx-proof">
          <ShieldCheck />
          Terminal authorization stays end to end. Cloud services only manage
          connectivity.
        </p>
      </section>
      <aside>
        <Section title="Account" />
        <div className="wx-person">
          <b>{value.profile.display_name}</b>
          <span>{value.profile.email}</span>
        </div>
        <dl className="wx-meta">
          <dt>ACCOUNT ID</dt>
          <dd>{value.profile.account_id}</dd>
          <dt>ORDERS</dt>
          <dd>{value.billing.orders.length}</dd>
        </dl>
      </aside>
      <section className="wx-activity">
        <Section title="Recent activity" icon={<Activity />} />
        {value.audit.map((a) => (
          <div className="wx-event" key={a.id}>
            <span>
              <b>{a.action.replace(".", " ")}</b>
              <small>{a.resource_id}</small>
            </span>
            <time>{when(a.occurred_at)}</time>
          </div>
        ))}
      </section>
    </div>
  );
}
function Section({
  title,
  end,
  icon,
}: {
  title: string;
  end?: string;
  icon?: ReactNode;
}) {
  return (
    <header className="wx-section">
      <span>{title}</span>
      {end ? <b>{end}</b> : icon}
    </header>
  );
}
function Row({
  label,
  value,
  live,
}: {
  label: string;
  value: string;
  live?: boolean;
}) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>
        {live && <i />}
        {value}
      </dd>
    </div>
  );
}
function Nodes({
  value,
  busy,
  action,
}: {
  value: Node[];
  busy: string;
  action: (k: string, f: () => Promise<Response>) => Promise<void>;
}) {
  return (
    <section>
      <Section
        title="Managed nodes"
        end={`${value.filter((n) => n.online && !n.revoked).length} ONLINE`}
      />
      <p className="wx-note">
        Revoking removes cloud directory access. Terminal grants remain
        daemon-owned.
      </p>
      <div className="wx-table wx-node-table">
        <div className="head">
          <span>NODE</span>
          <span>UPDATED</span>
          <span>STATUS</span>
          <span />
        </div>
        {value.map((n) => {
          const status = n.revoked
            ? "REVOKED"
            : n.online
              ? "ONLINE"
              : "OFFLINE";
          return (
            <div className="row" key={n.id}>
              <span>
                <Laptop />
                <b>{n.name}</b>
                <small>{n.kind}</small>
              </span>
              <time>{when(n.updated_at)}</time>
              <em className={status.toLowerCase()}>{status}</em>
              <span>
                {!n.revoked && (
                  <button
                    disabled={busy === n.id}
                    onClick={() =>
                      action(n.id, () =>
                        request("/api/nodes/revoke", { node_id: n.id }),
                      )
                    }
                  >
                    REVOKE
                  </button>
                )}
              </span>
            </div>
          );
        })}
      </div>
    </section>
  );
}
function BillingView({
  value,
  busy,
  action,
}: {
  value: Billing;
  busy: string;
  action: (k: string, f: () => Promise<Response>) => Promise<void>;
}) {
  async function checkout() {
    const c = await request("/api/checkout", { plan_id: "pro" });
    if (!c.ok) return c;
    return request("/api/checkout/confirm", { order_id: (await c.json()).id });
  }
  return (
    <div>
      <Section title="Subscription" />
      <div className="wx-plan">
        <span>CURRENT PLAN</span>
        <h2>{value.plan_id === "pro" ? "TermX Pro" : "Managed Free"}</h2>
        <p>
          {value.valid_until
            ? `Active until ${date(value.valid_until)}`
            : "Direct connectivity and managed signaling included."}
        </p>
        {value.plan_id !== "pro" && (
          <button
            disabled={busy === "checkout"}
            onClick={() => action("checkout", checkout)}
          >
            {busy === "checkout" ? "PROCESSING" : "TEST PRO CHECKOUT"}
          </button>
        )}
      </div>
      <Section title="Order history" />
      <div className="wx-events">
        {value.orders.length ? (
          value.orders.map((o) => (
            <div className="wx-event" key={o.id}>
              <span>
                <b>{o.plan_id.toUpperCase()}</b>
                <small>{o.id}</small>
              </span>
              <time>
                {date(o.created_at)} / {o.status}
              </time>
            </div>
          ))
        ) : (
          <Empty text="No orders yet" />
        )}
      </div>
    </div>
  );
}
function Account({
  value,
  busy,
  action,
}: {
  value: Profile;
  busy: string;
  action: (k: string, f: () => Promise<Response>) => Promise<void>;
}) {
  const [name, setName] = useState(value.display_name), [currentPassword, setCurrentPassword] = useState(""), [newPassword, setNewPassword] = useState("");
  return (
    <section className="wx-form-wrap">
      <Section title="Profile" />
      <form
        className="wx-form"
        onSubmit={(e) => {
          e.preventDefault();
          void action("profile", () =>
            request("/api/profile", { display_name: name }, "PATCH"),
          );
        }}
      >
        <label>
          DISPLAY NAME
          <input value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label>
          EMAIL
          <input value={value.email} disabled />
          <small>Managed by your identity provider.</small>
        </label>
        <div className="wx-identity">
          <ShieldCheck />
          <span>
            ACCOUNT IDENTITY
            <small>
              {value.user_id} / {value.account_id}
            </small>
          </span>
        </div>
        <button disabled={busy === "profile"}>
          {busy === "profile" ? "SAVING" : "SAVE CHANGES"}
        </button>
      </form>
      <Section title={value.password_configured ? "Change password" : "Set password"} />
      <form className="wx-form" onSubmit={(e)=>{e.preventDefault();void action("password",()=>request("/api/password",{current_password:currentPassword,new_password:newPassword})).then(()=>{setCurrentPassword("");setNewPassword("")})}}>
        {value.password_configured && <label>CURRENT PASSWORD<input required type="password" autoComplete="current-password" value={currentPassword} onChange={(e)=>setCurrentPassword(e.target.value)}/></label>}
        <label>NEW PASSWORD<input required minLength={10} type="password" autoComplete="new-password" value={newPassword} onChange={(e)=>setNewPassword(e.target.value)}/><small>Use at least 10 characters.</small></label>
        <button disabled={busy === "password"}>{busy === "password" ? "SAVING" : value.password_configured ? "CHANGE PASSWORD" : "SET PASSWORD"}</button>
      </form>
    </section>
  );
}
function Referrals({value}:{value:ReferralProgram}) {
  const link = typeof window === "undefined" ? `?aff=${value.code}` : `${window.location.origin}/login?aff=${value.code}`;
  return (
    <div>
      <Section title="Invite and earn" end={`${value.reward_days} BONUS DAYS`} />
      <div className="wx-plan"><span>YOUR AFF LINK</span><h2>{value.code}</h2><p>{link}</p><button onClick={()=>navigator.clipboard.writeText(link)}>COPY LINK</button></div>
      <dl className="wx-ledger"><Row label="Successful referrals" value={`${value.referred_count}`}/><Row label="Your reward" value="15 days after first payment"/><Row label="Friend reward" value="7 days after first payment"/></dl>
      <Section title="Reward history" />
      <div>{value.rewards.length ? value.rewards.map(r=><div className="wx-event" key={r.id}><span><b>{r.kind === "referrer" ? "Referral paid" : "Welcome reward"}</b><small>{r.order_id}</small></span><em>+{r.days} DAYS</em><time>{date(r.created_at)}</time></div>) : <Empty text="No rewards yet"/>}</div>
    </div>
  );
}
function Empty({ text }: { text: string }) {
  return <p className="wx-empty">{text}</p>;
}
