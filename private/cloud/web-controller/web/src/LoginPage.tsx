import { ArrowRight, Code2, ShieldCheck, UserRound } from "lucide-react";
import { FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";

interface Provider { id: string; name: string; configured: boolean; }

export default function LoginPage() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false), [error, setError] = useState(""),
    [providers, setProviders] = useState<Provider[]>([]), [mode, setMode] = useState<"login" | "register">("login"),
    [email, setEmail] = useState(""), [password, setPassword] = useState(""), [aff, setAff] = useState("");

  useEffect(() => {
    document.documentElement.dataset.wxTheme = localStorage.getItem("termx-wx-theme") === "neutral-dark" ? "neutral-dark" : "light-gray";
    setAff(new URLSearchParams(location.search).get("aff") ?? "");
    fetch("/api/providers").then((response) => response.json()).then(setProviders).catch(() => setProviders([]));
  }, []);

  function destination() {
    const next = new URLSearchParams(location.search).get("next") ?? "";
    return next.startsWith("/device?") ? next : "/account";
  }

  async function login() {
    setLoading(true); setError("");
    const response = await fetch("/api/auth/login", { method: "POST" });
    if (response.ok) location.href = destination();
    else { setError(t("login.providerError")); setLoading(false); }
  }

  async function passwordAuth(event: FormEvent) {
    event.preventDefault(); setLoading(true); setError("");
    const path = mode === "register" ? "/api/auth/password/register" : "/api/auth/password/login";
    const response = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ email, password, aff }) });
    if (response.ok) location.href = destination();
    else { const body = await response.json().catch(() => ({ error: t("login.authError") })); setError(body.error); setLoading(false); }
  }

  return (
    <main data-theme-surface className="grid min-h-dvh bg-background text-foreground lg:grid-cols-[minmax(0,1fr)_520px]">
      <section className="relative hidden min-h-dvh overflow-hidden border-r border-line bg-background lg:block">
        <i className="absolute inset-y-0 left-[32%] w-px bg-line" /><i className="absolute inset-x-0 top-[44%] h-px bg-line" />
        <a className="absolute left-8 top-7 z-10 flex items-center gap-3" href="/"><b className="grid size-8 place-items-center bg-primary font-mono text-[11px] font-medium text-primary-foreground">TX</b><span className="grid text-sm font-medium">TERMX<small className="text-[9px] font-normal text-muted-foreground">{t("login.managedEdge")}</small></span></a>
        <div className="absolute inset-x-10 top-[30%] z-10 grid grid-cols-[auto_minmax(120px,1fr)_auto] items-center gap-5 border border-line bg-panel p-5 font-mono text-[10px]">
          <span>{t("login.client")}<small className="mt-1.5 block text-[8px] text-success">{t("login.verified")}</small></span>
          <span className="grid grid-cols-[auto_1fr] items-center gap-2.5 text-[8px] text-success">{t("login.direct")}<i className="h-px bg-success" /><small className="col-span-2 text-[7px] text-muted-foreground">{t("login.relayStandby")}</small></span>
          <span>{t("login.daemon")}<small className="mt-1.5 block text-[8px] text-success">{t("login.owner")}</small></span>
        </div>
        <article className="absolute bottom-10 left-10 z-10 max-w-[620px]">
          <p className="font-mono text-[10px] text-success">{t("login.paths")}</p>
          <h1 className="my-5 text-[44px] font-light leading-[1.08]">{t("login.title1")}<br />{t("login.title2")}</h1>
          <p className="max-w-[560px] text-sm leading-7 text-muted-foreground">{t("login.copy")}</p>
          <dl className="mt-7 grid max-w-[560px] grid-cols-3 border border-line">
            {[[t("login.transport"), "P2P"], [t("login.fallback"), "RELAY"], [t("login.security"), "E2E"]].map(([label, value]) => <div className="border-r border-line p-4 last:border-r-0" key={label}><dt className="font-mono text-[9px] text-muted-foreground">{label}</dt><dd className="mt-1.5 font-mono text-xs">{value}</dd></div>)}
          </dl>
        </article>
      </section>

      <section className="flex min-h-dvh items-center px-5 py-12 sm:px-12">
        <div className="mx-auto w-full max-w-[360px]">
          <div className="mb-16 flex items-center justify-between lg:mb-10 lg:justify-end"><a className="flex items-center gap-3 lg:hidden" href="/"><b className="grid size-10 place-items-center bg-primary text-sm font-medium text-primary-foreground">TX</b><span className="font-medium">TERMX</span></a><LanguageSwitcher /></div>
          <p className="font-mono text-[10px] text-primary">{t("login.controller")}</p>
          <h2 className="mt-4 text-4xl font-light">{t("login.welcome")}</h2>
          <p className="mt-4 text-sm leading-6 text-muted-foreground">{t("login.intro")}</p>
          <div className="mt-8 grid gap-2.5">
            {providers.map((provider) => { const Icon = provider.id === "github" ? Code2 : UserRound; return <Button className="justify-start text-left text-xs" variant="outline" key={provider.id} disabled={!provider.configured}><Icon />{t("login.continueWith", { provider: provider.name })} {!provider.configured && <small className="ml-auto text-[9px] text-muted-foreground">{t("common.unavailable")}</small>}</Button>; })}
          </div>
          <div className="mt-6 grid grid-cols-2 border border-line">
            <button className={`min-h-11 border-b-2 px-2 text-xs ${mode === "login" ? "border-primary bg-panel" : "border-transparent text-muted-foreground"}`} onClick={() => setMode("login")}>{t("login.signInTab")}</button>
            <button className={`min-h-11 border-b-2 px-2 text-xs ${mode === "register" ? "border-primary bg-panel" : "border-transparent text-muted-foreground"}`} onClick={() => setMode("register")}>{t("login.createTab")}</button>
          </div>
          <form className="mt-5 grid gap-4" onSubmit={passwordAuth}>
            <Field label={t("login.email")}><Input required type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} /></Field>
            <Field label={t("login.password")}><Input required minLength={10} type="password" autoComplete={mode === "login" ? "current-password" : "new-password"} value={password} onChange={(event) => setPassword(event.target.value)} /></Field>
            {mode === "register" && aff && <Field label={t("login.affCode")}><Input value={aff} readOnly /></Field>}
            <Button className="mt-1 justify-between text-left" disabled={loading}>{loading ? t("login.wait") : mode === "login" ? t("login.signInEmail") : t("login.createAccount")}<ArrowRight /></Button>
          </form>
          <div className="my-6 flex items-center gap-3 text-center font-mono text-[9px] text-muted-foreground"><i className="h-px flex-1 bg-line" />{t("login.stagingIdentity")}<i className="h-px flex-1 bg-line" /></div>
          <Button className="w-full justify-between text-left" disabled={loading} onClick={login}>{loading ? t("login.signingIn") : t("login.continueStaging")}<ArrowRight /></Button>
          {error && <p className="mt-4 border border-destructive p-3 text-xs text-destructive" role="alert">{error}</p>}
          <footer className="mt-6 flex min-h-20 items-center gap-3 bg-inverse px-5 font-mono text-[8px] text-inverse-foreground"><ShieldCheck />{t("login.session")}</footer>
        </div>
      </section>
    </main>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="grid gap-2 font-mono text-[9px] text-muted-foreground">{label}{children}</label>;
}
