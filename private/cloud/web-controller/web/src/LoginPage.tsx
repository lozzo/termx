import { ArrowRight, Code2, ShieldCheck, UserRound } from "lucide-react";
import { FormEvent, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

interface Provider { id: string; name: string; configured: boolean; }

export default function LoginPage() {
  const [loading, setLoading] = useState(false), [error, setError] = useState(""),
    [providers, setProviders] = useState<Provider[]>([]), [mode, setMode] = useState<"login" | "register">("login"),
    [email, setEmail] = useState(""), [password, setPassword] = useState(""), [aff, setAff] = useState("");

  useEffect(() => {
    document.documentElement.dataset.wxTheme = localStorage.getItem("termx-wx-theme") === "neutral-dark" ? "neutral-dark" : "light-gray";
    setAff(new URLSearchParams(location.search).get("aff") ?? "");
    fetch("/api/providers").then((r) => r.json()).then(setProviders).catch(() => setProviders([]));
  }, []);

  async function login() {
    setLoading(true); setError("");
    const response = await fetch("/api/auth/login", { method: "POST" });
    if (response.ok) location.href = "/account";
    else { setError("Development identity provider is unavailable."); setLoading(false); }
  }

  async function passwordAuth(event: FormEvent) {
    event.preventDefault(); setLoading(true); setError("");
    const path = mode === "register" ? "/api/auth/password/register" : "/api/auth/password/login";
    const response = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ email, password, aff }) });
    if (response.ok) location.href = "/account";
    else { const body = await response.json().catch(() => ({ error: "Authentication failed" })); setError(body.error); setLoading(false); }
  }

  return (
    <main data-theme-surface className="grid min-h-dvh bg-background text-foreground lg:grid-cols-[minmax(0,1fr)_520px]">
      <section className="relative hidden min-h-dvh overflow-hidden border-r border-line bg-background lg:block">
        <i className="absolute inset-y-0 left-[32%] w-px bg-line" /><i className="absolute inset-x-0 top-[44%] h-px bg-line" />
        <a className="absolute left-8 top-7 z-10 flex items-center gap-3" href="/"><b className="grid size-8 place-items-center bg-primary font-mono text-[11px] font-medium text-primary-foreground">TX</b><span className="grid text-sm font-medium">TERMX<small className="text-[9px] font-normal text-muted-foreground">MANAGED EDGE</small></span></a>
        <div className="absolute inset-x-10 top-[30%] z-10 grid grid-cols-[auto_minmax(120px,1fr)_auto] items-center gap-5 border border-line bg-panel p-5 font-mono text-[10px]">
          <span>CLIENT<small className="mt-1.5 block text-[8px] text-success">IDENTITY VERIFIED</small></span>
          <span className="grid grid-cols-[auto_1fr] items-center gap-2.5 text-[8px] text-success">DIRECT P2P<i className="h-px bg-success" /><small className="col-span-2 text-[7px] text-muted-foreground">SINGLE RELAY / STANDBY</small></span>
          <span>DAEMON<small className="mt-1.5 block text-[8px] text-success">TERMINAL OWNER</small></span>
        </div>
        <article className="absolute bottom-10 left-10 z-10 max-w-[620px]">
          <p className="font-mono text-[10px] text-success">CLOUD AVAILABLE / DIRECT / RELAY / E2E</p>
          <h1 className="my-5 text-[44px] font-light leading-[1.08]">Your terminal network,<br />under your control.</h1>
          <p className="max-w-[560px] text-sm leading-7 text-muted-foreground">Reach registered machines and manage cloud services without exposing terminal authorization.</p>
          <dl className="mt-7 grid max-w-[560px] grid-cols-3 border border-line">
            {[["TRANSPORT", "P2P"], ["FALLBACK", "RELAY"], ["SECURITY", "E2E"]].map(([label, value]) => <div className="border-r border-line p-4 last:border-r-0" key={label}><dt className="font-mono text-[9px] text-muted-foreground">{label}</dt><dd className="mt-1.5 font-mono text-xs">{value}</dd></div>)}
          </dl>
        </article>
      </section>

      <section className="flex min-h-dvh items-center px-5 py-12 sm:px-12">
        <div className="mx-auto w-full max-w-[360px]">
          <a className="mb-20 flex items-center gap-3 lg:hidden" href="/"><b className="grid size-10 place-items-center bg-primary text-sm font-medium text-primary-foreground">TX</b><span className="font-medium">TERMX</span></a>
          <p className="font-mono text-[10px] text-primary">WEB CONTROLLER</p>
          <h2 className="mt-4 text-4xl font-light">Welcome back</h2>
          <p className="mt-4 text-sm leading-6 text-muted-foreground">Sign in to manage nodes, subscriptions, account settings, and referral rewards.</p>
          <div className="mt-8 grid gap-2.5">
            {providers.map((provider) => { const Icon = provider.id === "github" ? Code2 : UserRound; return <Button className="justify-start text-xs" variant="outline" key={provider.id} disabled={!provider.configured}><Icon />CONTINUE WITH {provider.name.toUpperCase()} {!provider.configured && <small className="ml-auto text-[9px] text-muted-foreground">UNAVAILABLE</small>}</Button>; })}
          </div>
          <div className="mt-6 grid grid-cols-2 border border-line">
            <button className={`min-h-11 border-b-2 text-xs ${mode === "login" ? "border-primary bg-panel" : "border-transparent text-muted-foreground"}`} onClick={() => setMode("login")}>SIGN IN</button>
            <button className={`min-h-11 border-b-2 text-xs ${mode === "register" ? "border-primary bg-panel" : "border-transparent text-muted-foreground"}`} onClick={() => setMode("register")}>CREATE ACCOUNT</button>
          </div>
          <form className="mt-5 grid gap-4" onSubmit={passwordAuth}>
            <Field label="EMAIL"><Input required type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} /></Field>
            <Field label="PASSWORD"><Input required minLength={10} type="password" autoComplete={mode === "login" ? "current-password" : "new-password"} value={password} onChange={(event) => setPassword(event.target.value)} /></Field>
            {mode === "register" && aff && <Field label="AFF CODE"><Input value={aff} readOnly /></Field>}
            <Button className="mt-1 justify-between" disabled={loading}>{loading ? "PLEASE WAIT" : mode === "login" ? "SIGN IN WITH EMAIL" : "CREATE ACCOUNT"}<ArrowRight /></Button>
          </form>
          <div className="my-6 flex items-center gap-3 font-mono text-[9px] text-muted-foreground"><i className="h-px flex-1 bg-line" />STAGING IDENTITY<i className="h-px flex-1 bg-line" /></div>
          <Button className="w-full justify-between" disabled={loading} onClick={login}>{loading ? "SIGNING IN" : "CONTINUE WITH STAGING ACCOUNT"}<ArrowRight /></Button>
          {error && <p className="mt-4 border border-destructive p-3 text-xs text-destructive" role="alert">{error}</p>}
          <footer className="mt-6 flex min-h-20 items-center gap-3 bg-inverse px-5 font-mono text-[8px] text-inverse-foreground"><ShieldCheck />HTTPONLY SESSION / SAMESITE STRICT / CSRF PROTECTED</footer>
        </div>
      </section>
    </main>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="grid gap-2 font-mono text-[9px] text-muted-foreground">{label}{children}</label>;
}
