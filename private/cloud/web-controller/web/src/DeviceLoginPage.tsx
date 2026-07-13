import { ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { Button } from "@/components/ui/button";

interface DeviceLoginRequest { user_code: string; expires_at: string; }

const csrf = () => document.cookie.split("; ").find((value) => value.startsWith("termx_csrf="))?.split("=")[1] ?? "";

export default function DeviceLoginPage() {
  const { t } = useTranslation();
  const code = new URLSearchParams(location.search).get("code") ?? "";
  const [request, setRequest] = useState<DeviceLoginRequest | null>(null), [state, setState] = useState<"loading" | "ready" | "approved" | "error">("loading");

  useEffect(() => {
    document.documentElement.dataset.wxTheme = localStorage.getItem("termx-wx-theme") === "neutral-dark" ? "neutral-dark" : "light-gray";
    fetch(`/api/device-login?code=${encodeURIComponent(code)}`, { cache: "no-store" }).then(async (response) => {
      if (response.status === 401) {
        location.href = `/login?next=${encodeURIComponent(location.pathname + location.search)}`;
        return;
      }
      if (!response.ok) throw new Error("unavailable");
      setRequest(await response.json()); setState("ready");
    }).catch(() => setState("error"));
  }, [code]);

  async function approve() {
    setState("loading");
    const response = await fetch("/api/device-login/approve", { method: "POST", headers: { "Content-Type": "application/json", "X-TermX-CSRF": csrf() }, body: JSON.stringify({ user_code: code }) });
    setState(response.ok ? "approved" : "error");
  }

  return <main data-theme-surface className="grid min-h-dvh place-items-center bg-background px-5 text-foreground">
    <section className="w-full max-w-[460px] border border-line bg-panel">
      <header className="flex h-16 items-center justify-between border-b border-line px-5"><a className="flex items-center gap-3 text-sm font-medium" href="/"><b className="grid size-8 place-items-center bg-primary font-mono text-[11px] text-primary-foreground">TX</b>TermX Cloud</a><LanguageSwitcher compact /></header>
      <div className="p-6 sm:p-8"><p className="font-mono text-[9px] text-primary">{t("device.kicker")}</p><h1 className="mt-3 text-3xl font-light">{state === "approved" ? t("device.approvedTitle") : t("device.title")}</h1>
        <p className="mt-4 text-sm leading-6 text-muted-foreground">{state === "approved" ? t("device.approvedCopy") : t("device.copy")}</p>
        {request && state !== "approved" && <div className="my-7 border border-line bg-background p-5"><span className="font-mono text-[9px] text-muted-foreground">{t("device.code")}</span><strong className="mt-2 block font-mono text-2xl font-normal">{request.user_code}</strong></div>}
        {state === "ready" && <Button className="w-full justify-center" onClick={approve}><ShieldCheck />{t("device.approve")}</Button>}
        {state === "loading" && <p className="mt-7 text-xs text-muted-foreground">{t("device.loading")}</p>}
        {state === "error" && <p className="mt-7 border border-destructive p-3 text-xs text-destructive">{t("device.error")}</p>}
      </div>
    </section>
  </main>;
}
