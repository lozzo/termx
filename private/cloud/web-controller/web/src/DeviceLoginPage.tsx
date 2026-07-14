import { ArrowRight, ShieldCheck } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

interface DeviceLoginRequest {
  user_code: string;
  expires_at: string;
  state: "waiting_for_device" | "waiting_for_approval";
  client_label?: string;
  client_platform?: string;
}

const csrf = () => document.cookie.split("; ").find((value) => value.startsWith("termx_csrf="))?.split("=")[1] ?? "";
const activationAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ";
const normalizeCode = (value: string) => Array.from(value.toUpperCase()).filter((character) => activationAlphabet.includes(character)).join("").slice(0, 10).replace(/^(.{5})(.+)$/, "$1-$2");

export default function DeviceLoginPage() {
  const { t } = useTranslation();
  const initialCode = normalizeCode(new URLSearchParams(location.search).get("code") ?? "");
  const [code, setCode] = useState(initialCode);
  const [submittedCode, setSubmittedCode] = useState(initialCode);
  const [request, setRequest] = useState<DeviceLoginRequest | null>(null);
  const [state, setState] = useState<"input" | "loading" | "ready" | "approved" | "error">(initialCode ? "loading" : "input");

  useEffect(() => {
    document.documentElement.dataset.wxTheme = localStorage.getItem("termx-wx-theme") === "neutral-dark" ? "neutral-dark" : "light-gray";
  }, []);

  useEffect(() => {
    if (!submittedCode) return;
    let stopped = false;
    async function inspect() {
      try {
        const response = await fetch(`/api/device-login?code=${encodeURIComponent(submittedCode)}`, { cache: "no-store" });
        if (response.status === 401) {
          location.href = `/login?next=${encodeURIComponent(`/device?code=${submittedCode}`)}`;
          return;
        }
        if (!response.ok) throw new Error("unavailable");
        const value = await response.json() as DeviceLoginRequest;
        if (!stopped) { setRequest(value); setState(value.state === "waiting_for_approval" ? "ready" : "loading"); }
      } catch {
        if (!stopped) setState("error");
      }
    }
    void inspect();
    const timer = window.setInterval(inspect, 1500);
    return () => { stopped = true; window.clearInterval(timer); };
  }, [submittedCode]);

  async function approve() {
    setState("loading");
    const response = await fetch("/api/device-login/approve", { method: "POST", headers: { "Content-Type": "application/json", "X-TermX-CSRF": csrf() }, body: JSON.stringify({ user_code: submittedCode }) });
    if (response.ok) {
      setSubmittedCode("");
      setState("approved");
    } else {
      setState("error");
    }
  }

  function inspectCode(event: FormEvent) {
    event.preventDefault();
    if (code.length !== 11) return;
    setRequest(null);
    setState("loading");
    setSubmittedCode(code);
    history.replaceState(null, "", `/device?code=${encodeURIComponent(code)}`);
  }

  return <main data-theme-surface className="grid min-h-dvh place-items-center bg-background px-5 text-foreground">
    <section className="w-full max-w-[460px] border border-line bg-panel">
      <header className="flex h-16 items-center justify-between border-b border-line px-5"><a className="flex items-center gap-3 text-sm font-medium" href="/"><b className="grid size-8 place-items-center bg-primary font-mono text-[11px] text-primary-foreground">TX</b>TermX Cloud</a><LanguageSwitcher compact /></header>
      <div className="p-6 sm:p-8">
        <p className="font-mono text-[9px] text-primary">{t("device.kicker")}</p>
        <h1 className="mt-3 text-3xl font-light">{state === "approved" ? t("device.approvedTitle") : submittedCode ? t("device.title") : t("device.enterTitle")}</h1>
        <p className="mt-4 text-sm leading-6 text-muted-foreground">{state === "approved" ? t("device.approvedCopy") : submittedCode ? t("device.copy") : t("device.enterCopy")}</p>
        {!submittedCode && <form className="mt-7 grid gap-3" onSubmit={inspectCode}>
          <label className="grid gap-2 font-mono text-[9px] text-muted-foreground" htmlFor="device-code">{t("device.code")}</label>
          <Input id="device-code" autoCapitalize="characters" autoComplete="one-time-code" className="h-14 font-mono text-xl" value={code} onChange={(event) => setCode(normalizeCode(event.target.value))} placeholder="ABCDE-FGHIJ" />
          <Button className="mt-1 w-full justify-center" disabled={code.length !== 11}><ArrowRight />{t("device.continue")}</Button>
        </form>}
        {request && state !== "approved" && <div className="my-7 border-y border-line bg-background px-1 py-5"><span className="font-mono text-[9px] text-muted-foreground">{t("device.code")}</span><strong className="mt-2 block font-mono text-2xl font-normal">{request.user_code}</strong>{request.client_label && <p className="mt-4 text-xs text-muted-foreground">{request.client_label} · {request.client_platform}</p>}</div>}
        {state === "ready" && <Button className="w-full justify-center" onClick={approve}><ShieldCheck />{t("device.approve")}</Button>}
        {state === "loading" && <p className="mt-7 flex items-center gap-3 text-xs text-muted-foreground" aria-live="polite"><i className="size-2 animate-pulse bg-primary" />{request?.state === "waiting_for_device" ? t("device.waitingForDevice") : t("device.loading")}</p>}
        {state === "error" && <div className="mt-7"><p className="border border-destructive p-3 text-xs text-destructive" role="alert">{t("device.error")}</p><Button className="mt-3 w-full justify-center" variant="outline" onClick={() => { setSubmittedCode(""); setRequest(null); setState("input"); }}>{t("device.tryAnother")}</Button></div>}
      </div>
    </section>
  </main>;
}
