import { create } from "@bufbuild/protobuf";
import { LogOut, Menu, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import AccountPage from "./AccountPage";
import OperatorPage from "./OperatorPage";
import { Button } from "@/components/ui/button";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import {
  GetOperatorWorkspaceResponseSchema,
  type OperatorWorkspaceModule,
} from "@/generated/cloudpb/cloud_management_pb";
import {
  LogoutAccountSessionRequestSchema,
  LogoutAccountSessionResponseSchema,
} from "@/generated/cloudpb/cloud_product_pb";
import { ProtoHTTPError, protoGet, protoPost } from "@/protoApi";
import { preferredLanguageForPath } from "@/i18n";
import {
  accountNavigationItems,
  operatorModuleFromPath,
  operatorNavigationItems,
  type AccountSection,
} from "@/consoleNavigation";

function routeLocation() {
  return `${window.location.pathname}${window.location.search}`;
}

function accountSectionFromLocation(): AccountSection {
  const section = new URLSearchParams(window.location.search).get("section");
  return accountNavigationItems.some((item) => item.key === section) ? section as AccountSection : "overview";
}

/**
 * ConsolePage 是登录后 Web 的唯一页面壳，拥有导航 URL、移动抽屉和 Workspace 可见性投影。
 * 账号与运营页面只渲染右侧内容；每个业务 API 仍由后端逐请求授权。
 */
export default function ConsolePage() {
  const { t, i18n } = useTranslation();
  const [routePath, setRoutePath] = useState(routeLocation);
  const [workspaceModules, setWorkspaceModules] = useState<OperatorWorkspaceModule[]>([]);
  const [workspaceLoaded, setWorkspaceLoaded] = useState(false);
  const [workspaceError, setWorkspaceError] = useState("");
  const [navigationOpen, setNavigationOpen] = useState(false);
  const workspaceRequestedRef = useRef(false);
  const navigationRef = useRef<HTMLElement>(null);
  const navigationTriggerRef = useRef<HTMLButtonElement>(null);

  const isOperatorRoute = window.location.pathname === "/operator" || window.location.pathname.startsWith("/operator/");
  const accountSection = accountSectionFromLocation();
  const activeOperatorModule = operatorModuleFromPath(window.location.pathname);
  const allowedOperatorItems = operatorNavigationItems.filter((item) => workspaceModules.includes(item.permission));

  function navigate(path: string, replace = false) {
    window.history[replace ? "replaceState" : "pushState"]({}, "", path);
    setRoutePath(routeLocation());
    setNavigationOpen(false);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }

  useEffect(() => {
    const onPopState = () => setRoutePath(routeLocation());
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  useEffect(() => {
    void i18n.changeLanguage(preferredLanguageForPath(window.location.pathname));
  }, [isOperatorRoute, i18n]);

  useEffect(() => {
    if (workspaceRequestedRef.current) return;
    workspaceRequestedRef.current = true;
    void (async () => {
      try {
        const workspace = await protoGet("/api/v1/operator/workspace", GetOperatorWorkspaceResponseSchema);
        setWorkspaceModules(workspace.modules);
      } catch (cause) {
        if (cause instanceof ProtoHTTPError && cause.status === 401) {
          location.href = "/login";
          return;
        }
        if (!(cause instanceof ProtoHTTPError && cause.status === 403))
          setWorkspaceError(cause instanceof Error ? cause.message : i18n.t("account.requestFailed"));
      } finally {
        setWorkspaceLoaded(true);
      }
    })();
  }, []);

  useEffect(() => {
    if (!workspaceLoaded || !isOperatorRoute) return;
    const currentAllowed = allowedOperatorItems.some((item) => item.key === activeOperatorModule);
    if (currentAllowed) return;
    navigate(allowedOperatorItems[0] ? `/operator/${allowedOperatorItems[0].key}` : "/account", true);
  }, [routePath, workspaceLoaded]);

  useEffect(() => {
    if (!navigationOpen) return;
    const previousOverflow = document.body.style.overflow;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setNavigationOpen(false);
        return;
      }
      if (event.key !== "Tab" || !navigationRef.current) return;
      const focusable = Array.from(navigationRef.current.querySelectorAll<HTMLElement>("button:not([disabled]), a[href]"));
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (!first || !last) return;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.body.style.overflow = "hidden";
    window.addEventListener("keydown", onKeyDown);
    requestAnimationFrame(() => {
      (navigationRef.current?.querySelector<HTMLElement>("a[aria-current='page']") ?? navigationRef.current?.querySelector<HTMLElement>("a[href], button"))?.focus();
    });
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", onKeyDown);
      navigationTriggerRef.current?.focus();
    };
  }, [navigationOpen]);

  async function logout() {
    await protoPost(
      "/api/v1/account/logout",
      LogoutAccountSessionRequestSchema,
      create(LogoutAccountSessionRequestSchema),
      LogoutAccountSessionResponseSchema,
    );
    location.href = "/login";
  }

  return (
    <div className="min-h-dvh bg-background text-foreground md:grid md:grid-cols-[248px_minmax(0,1fr)]">
      <a className="sr-only focus:fixed focus:left-3 focus:top-3 focus:z-50 focus:bg-primary focus:px-4 focus:py-3 focus:text-primary-foreground" href="#console-content">{t("account.skip")}</a>
      {navigationOpen && <button className="fixed inset-0 z-30 bg-foreground/20 md:hidden" aria-label={t("operator.navigation.close")} onClick={() => setNavigationOpen(false)} />}
      <aside ref={navigationRef} id="console-navigation" className={`${navigationOpen ? "translate-x-0" : "-translate-x-full"} fixed inset-y-0 left-0 z-40 flex w-[min(19rem,86vw)] flex-col border-r border-line bg-panel transition-transform motion-reduce:transition-none md:sticky md:top-0 md:h-dvh md:w-auto md:translate-x-0 md:transition-none`}>
        <div className="flex min-h-20 items-center justify-between gap-3 border-b border-line px-5">
          <a className="flex min-w-0 items-center gap-3" href="/" aria-label={t("common.home")}>
            <b className="grid size-9 shrink-0 place-items-center bg-primary font-mono text-xs text-primary-foreground">MV</b>
            <span className="min-w-0"><strong className="block truncate text-sm">Muxvia Cloud</strong><small className="block truncate text-[10px] text-muted-foreground">{t("account.cloudControl")}</small></span>
          </a>
          <Button className="md:hidden" variant="ghost" size="icon" aria-label={t("operator.navigation.close")} onClick={() => setNavigationOpen(false)}><X /></Button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-3 py-4">
          <nav aria-label={t("common.primaryNavigation")}>
            {accountNavigationItems.map(({ key, icon: Icon }) => (
              <a key={key} href={`/account?section=${key}`} aria-current={!isOperatorRoute && accountSection === key ? "page" : undefined} className={`relative flex min-h-11 items-center gap-3 border-b border-line px-3 text-sm focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-primary ${!isOperatorRoute && accountSection === key ? "bg-soft font-semibold text-primary" : "text-muted-foreground hover:bg-soft/70 hover:text-foreground"}`} onClick={(event) => { event.preventDefault(); navigate(`/account?section=${key}`); }}>
                <span className={`absolute inset-y-2 left-0 w-0.5 ${!isOperatorRoute && accountSection === key ? "bg-primary" : "bg-transparent"}`} />
                <Icon className="size-4 shrink-0" />
                <span>{t(`account.tabs.${key}`)}</span>
              </a>
            ))}
          </nav>
          {allowedOperatorItems.length > 0 && (
            <section className="mt-7">
              <p className="px-3 pb-2 font-mono text-[10px] font-semibold text-muted-foreground">{t("account.admin.navigation")}</p>
              <nav aria-label={t("operator.navigation.label")}>
                {allowedOperatorItems.map(({ key, icon: Icon }) => (
                  <a key={key} href={`/operator/${key}`} aria-current={isOperatorRoute && activeOperatorModule === key ? "page" : undefined} className={`relative flex min-h-11 items-center gap-3 border-b border-line px-3 text-sm focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-primary ${isOperatorRoute && activeOperatorModule === key ? "bg-soft font-semibold text-primary" : "text-muted-foreground hover:bg-soft/70 hover:text-foreground"}`} onClick={(event) => { event.preventDefault(); navigate(`/operator/${key}`); }}>
                    <span className={`absolute inset-y-2 left-0 w-0.5 ${isOperatorRoute && activeOperatorModule === key ? "bg-primary" : "bg-transparent"}`} />
                    <Icon className="size-4 shrink-0" />
                    <span>{t(`operator.navigation.modules.${key}`)}</span>
                  </a>
                ))}
              </nav>
            </section>
          )}
        </div>
        <div className="grid gap-2 border-t border-line p-3">
          <LanguageSwitcher compact />
          <Button className="w-full justify-start" variant="ghost" onClick={() => void logout()}><LogOut />{t("account.signOut")}</Button>
        </div>
      </aside>
      <div className="min-w-0">
        <header className="sticky top-0 z-20 flex min-h-14 items-center justify-between border-b border-line bg-panel px-4 md:hidden">
          <Button ref={navigationTriggerRef} variant="outline" size="icon" aria-label={t("operator.navigation.open")} aria-controls="console-navigation" aria-expanded={navigationOpen} onClick={() => setNavigationOpen(true)}><Menu /></Button>
          <strong className="text-sm">Muxvia Cloud</strong>
          <span className="size-9" aria-hidden="true" />
        </header>
        <main className="min-w-0 p-4 sm:p-6 md:p-8 xl:p-10" id="console-content">
          {workspaceError && <p className="mb-5 border border-destructive p-3 text-xs text-destructive" role="alert">{workspaceError}</p>}
          {isOperatorRoute ? (
            workspaceLoaded && activeOperatorModule && allowedOperatorItems.some((item) => item.key === activeOperatorModule)
              ? <OperatorPage />
              : <div className="border border-line bg-panel p-6 text-sm text-muted-foreground" role="status">{t("operator.loading")}</div>
          ) : <AccountPage section={accountSection} />}
        </main>
      </div>
    </div>
  );
}
