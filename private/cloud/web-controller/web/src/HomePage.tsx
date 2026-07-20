import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
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
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { intlLocale } from "@/i18n";
import {
  CatalogPriceMode,
  GetPlanCatalogResponseSchema,
  type GetPlanCatalogResponse,
} from "@/generated/cloudpb/cloud_product_pb";
import { protoGet } from "@/protoApi";

type Theme = "light-gray" | "neutral-dark";

export default function HomePage() {
  const { t, i18n } = useTranslation();
  const [catalog, setCatalog] = useState<GetPlanCatalogResponse | null>(null);
  const [theme, setTheme] = useState<Theme>(() =>
    localStorage.getItem("termx-wx-theme") === "neutral-dark"
      ? "neutral-dark"
      : "light-gray",
  );

  useEffect(() => {
    protoGet("/api/v1/catalog", GetPlanCatalogResponseSchema)
      .then(setCatalog)
      .catch(() => setCatalog(null));
  }, []);

  function selectTheme(next: Theme) {
    setTheme(next);
    document.documentElement.dataset.wxTheme = next;
    localStorage.setItem("termx-wx-theme", next);
  }

  const coreFeatures = [Terminal, History, Network, Folder].map(
    (icon, index) => ({
      icon,
      title: t(`home.features.items.${index}.title`),
      copy: t(`home.features.items.${index}.copy`),
    }),
  );
  const productLayers = [
    {
      id: "core",
      icon: Terminal,
      action: { href: "https://github.com/lozzo/termx", external: true },
    },
    { id: "app", icon: Smartphone },
    { id: "cloud", icon: Cloud, action: { href: "#plans", external: false } },
  ].map((product) => ({
    ...product,
    eyebrow: t(`home.products.${product.id}.eyebrow`),
    title: t(`home.products.${product.id}.title`),
    copy: t(`home.products.${product.id}.copy`),
    points: t(`home.products.${product.id}.points`, {
      returnObjects: true,
    }) as string[],
    action: product.action
      ? { ...product.action, label: t(`home.products.${product.id}.action`) }
      : undefined,
  }));

  return (
    <main
      data-theme-surface
      className="min-w-0 overflow-hidden bg-background text-foreground"
    >
      <header className="sticky top-0 z-40 h-[66px] border-b border-line bg-background/95 backdrop-blur-xl">
        <div className="mx-auto grid h-full w-[min(1360px,calc(100%_-_48px))] grid-cols-[1fr_auto_1fr] items-center max-md:w-[calc(100%_-_28px)] max-md:grid-cols-[1fr_auto]">
          <Brand href="#top" />
          <nav
            className="flex items-center gap-6 text-[9px] font-semibold text-muted-foreground max-md:hidden"
            aria-label={t("common.primaryNavigation")}
          >
            <a href="#product">{t("home.nav.product")}</a>
            <a href="#app">{t("home.nav.app")}</a>
            <a href="#cloud">{t("home.nav.cloud")}</a>
            <a href="#plans">{t("home.nav.plans")}</a>
            <a
              className="flex items-center gap-1"
              href="https://github.com/lozzo/termx"
              target="_blank"
              rel="noreferrer"
            >
              {t("common.source")} <ExternalLink className="size-2.5" />
            </a>
          </nav>
          <div className="justify-self-end flex items-center gap-3">
            <LanguageSwitcher compact />
            <div
              className="flex h-9 border border-line"
              aria-label={t("common.colorTheme")}
            >
              <button
                className={cn(
                  "grid w-9 place-items-center border-r border-line bg-panel text-muted-foreground max-md:w-11",
                  theme === "light-gray" && "bg-soft text-foreground",
                )}
                onClick={() => selectTheme("light-gray")}
                aria-label={t("common.useLight")}
                title={t("common.light")}
              >
                <Sun className="size-3.5" />
              </button>
              <button
                className={cn(
                  "grid w-9 place-items-center bg-panel text-muted-foreground max-md:w-11",
                  theme === "neutral-dark" && "bg-soft text-foreground",
                )}
                onClick={() => selectTheme("neutral-dark")}
                aria-label={t("common.useDark")}
                title={t("common.dark")}
              >
                <Moon className="size-3.5" />
              </button>
            </div>
            <a
              className={cn(buttonVariants({ size: "sm" }), "max-md:hidden")}
              href="/login"
            >
              {t("common.signIn")} <ArrowRight />
            </a>
          </div>
        </div>
      </header>

      <section
        className="min-h-[calc(100dvh-66px)] border-b border-line bg-background pt-20 max-md:min-h-0 max-md:pt-14"
        id="top"
      >
        <div
          className={`${wrap} grid grid-cols-[1.35fr_.65fr] gap-x-18 max-lg:grid-cols-2 max-lg:gap-x-10 max-md:grid-cols-1`}
        >
          <div>
            <Kicker>{t("home.hero.kicker")}</Kicker>
            <h1 className="m-0 text-[74px] font-light leading-[.9] max-md:text-[52px]">
              TermX
            </h1>
            <p className="mt-7 text-[46px] font-light leading-[1.08] max-md:mt-5 max-md:text-[34px]">
              {t("home.hero.thesis1")}
              <br />
              {t("home.hero.thesis2")}
            </p>
          </div>
          <div className="self-end pb-1 max-md:mt-8">
            <p className="m-0 text-[15px] leading-7 text-muted-foreground max-md:text-sm">
              {t("home.hero.intro")}
            </p>
            <div className="mt-7 grid gap-2">
              <a className={buttonVariants()} href="/login">
                {t("home.hero.cloudCta")} <ArrowRight />
              </a>
              <a
                className={buttonVariants({ variant: "outline" })}
                href="https://github.com/lozzo/termx"
                target="_blank"
                rel="noreferrer"
              >
                <GitFork /> {t("home.hero.coreCta")}
              </a>
            </div>
          </div>

          <div
            className="col-span-full mt-16 border border-b-0 border-line-strong bg-panel max-md:mt-12"
            aria-label={t("common.terminalPoolModel")}
          >
            <header className="flex min-h-11 items-center justify-between gap-3 border-b border-line px-4 text-[8px] text-muted-foreground">
              <span>{t("home.pool.title")}</span>
              <strong className="flex shrink-0 items-center gap-2 font-semibold text-success">
                <i className="size-1.5 rounded-full bg-success" />{" "}
                {t("home.pool.online")}
              </strong>
            </header>
            <div className="grid min-h-60 grid-cols-[310px_minmax(300px,1fr)_270px] max-lg:grid-cols-[250px_1fr] max-md:grid-cols-1">
              <div className="border-r border-line bg-soft max-md:border-r-0">
                <TerminalRow
                  active
                  name="api-dev"
                  meta={`${t("home.pool.running")} / 02:14:37`}
                  status={t("home.pool.live")}
                />
                <TerminalRow
                  name="agent-refactor"
                  meta={`${t("home.pool.running")} / 08:41:02`}
                  status={t("home.pool.live")}
                />
                <TerminalRow
                  name="release-check"
                  meta={t("home.pool.exited")}
                  status="137"
                />
              </div>
              <div className="flex min-h-60 flex-col justify-center px-11 py-9 max-md:border-t max-md:border-line max-md:px-6">
                <p className="m-0 text-[8px] font-semibold text-primary">
                  {t("home.pool.work")}
                </p>
                <strong className="mt-5 max-w-md text-2xl font-light leading-tight">
                  {t("home.pool.views")}
                </strong>
                <span className="mt-4 max-w-lg text-[11px] leading-5 text-muted-foreground">
                  {t("home.pool.detail")}
                </span>
              </div>
              <div className="border-l border-line p-6 max-lg:col-span-full max-lg:grid max-lg:grid-cols-[auto_1fr_1fr] max-lg:items-center max-lg:border-l-0 max-lg:border-t max-md:col-auto max-md:block">
                <p className="m-0 mb-4 text-[7px] text-muted-foreground">
                  {t("home.pool.available")}
                </p>
                <Observer
                  icon={<Laptop />}
                  title={t("home.pool.desktop")}
                  detail={t("home.pool.desktopPaths")}
                />
                <Observer
                  icon={<Smartphone />}
                  title={t("home.pool.officialApp")}
                  detail={t("home.pool.appPaths")}
                />
              </div>
            </div>
            <footer className="grid min-h-12 grid-cols-[auto_auto_auto_1fr] items-center gap-6 border-t border-line px-4 text-[7px] text-muted-foreground max-md:min-h-24 max-md:grid-cols-2 max-md:gap-3">
              <span>{t("home.pool.local")}</span>
              <span>{t("home.pool.ssh")}</span>
              <span>{t("home.pool.webrtc")}</span>
              <strong className="justify-self-end text-foreground max-md:justify-self-start">
                {t("home.pool.model")}
              </strong>
            </footer>
          </div>
        </div>
      </section>

      <section
        className="border-b border-line bg-panel py-28 max-md:py-20"
        id="product"
      >
        <div className={wrap}>
          <SectionHeading kicker={t("home.products.kicker")}>
            {t("home.products.title1")}
            <br />
            {t("home.products.title2")}
          </SectionHeading>
          <div className="mt-16 grid grid-cols-3 border-l border-t border-line-strong max-md:mt-11 max-md:grid-cols-1">
            {productLayers.map((product) => {
              const Icon = product.icon;
              return (
                <article
                  className="flex min-h-[430px] flex-col border-b border-r border-line-strong p-8 even:bg-soft max-lg:p-6"
                  id={
                    product.id === "app"
                      ? "app"
                      : product.id === "cloud"
                        ? "cloud"
                        : undefined
                  }
                  key={product.id}
                >
                  <header className="flex items-center gap-2.5 text-[8px] font-semibold text-primary">
                    <Icon className="size-4" />
                    <span>{product.eyebrow}</span>
                  </header>
                  <h3 className="mt-16 text-[28px] font-light">
                    {product.title}
                  </h3>
                  <p className="mt-4 min-h-20 text-[11px] leading-5 text-muted-foreground">
                    {product.copy}
                  </p>
                  <ul className="mt-6 flex-1 space-y-3 border-t border-line pt-6 text-[9px] text-muted-foreground">
                    {product.points.map((point) => (
                      <li className="flex gap-2" key={point}>
                        <Check className="size-3 text-success" /> {point}
                      </li>
                    ))}
                  </ul>
                  {product.action && (
                    <a
                      className="flex min-h-11 items-center justify-between border-t border-line text-[9px] font-semibold"
                      href={product.action.href}
                      target={product.action.external ? "_blank" : undefined}
                      rel={product.action.external ? "noreferrer" : undefined}
                    >
                      {product.action.label}{" "}
                      {product.action.external ? (
                        <ExternalLink className="size-3" />
                      ) : (
                        <ArrowRight className="size-3" />
                      )}
                    </a>
                  )}
                </article>
              );
            })}
          </div>
          <p className="m-0 flex min-h-16 items-center gap-3 border-x border-b border-line px-5 text-[10px] leading-5 text-muted-foreground">
            <ShieldCheck className="size-4 shrink-0 text-primary" />{" "}
            {t("home.products.boundary")}
          </p>
        </div>
      </section>

      <section className="border-b border-line bg-background py-28 max-md:py-20">
        <div className={wrap}>
          <SectionHeading kicker={t("home.features.kicker")}>
            {t("home.features.title1")}
            <br />
            {t("home.features.title2")}
          </SectionHeading>
          <div className="mt-16 border-t border-line-strong max-md:mt-11">
            {coreFeatures.map(({ icon: FeatureIcon, title, copy }, index) => (
              <article
                className="grid min-h-32 grid-cols-[45px_45px_240px_1fr] items-center gap-5 border-b border-line max-md:min-h-48 max-md:grid-cols-[28px_32px_1fr] max-md:gap-3 max-md:py-6"
                key={title}
              >
                <span className="text-[8px] text-primary">0{index + 1}</span>
                <FeatureIcon className="size-4 text-primary" />
                <h3 className="text-lg font-normal">{title}</h3>
                <p className="m-0 max-w-2xl text-xs leading-5 text-muted-foreground max-md:col-start-3">
                  {copy}
                </p>
              </article>
            ))}
          </div>
        </div>
      </section>

      <section
        className="border-b border-line bg-panel py-28 max-md:py-20"
        id="connectivity"
      >
        <div className={wrap}>
          <SectionHeading
            kicker={t("home.connectivity.kicker")}
            copy={t("home.connectivity.copy")}
          >
            {t("home.connectivity.title1")}
            <br />
            {t("home.connectivity.title2")}
          </SectionHeading>
          <div
            className="mt-16 grid min-h-72 grid-cols-[260px_1fr_260px] border border-line-strong max-lg:grid-cols-[210px_1fr_210px] max-md:mt-11 max-md:grid-cols-1"
            aria-label={t("common.cloudConnectionModel")}
          >
            <RouteNode
              icon={<Smartphone />}
              label={t("home.connectivity.client")}
              title={t("home.connectivity.app")}
              status={t("home.connectivity.identity")}
            />
            <div className="grid grid-rows-[1fr_55px_55px] border-l border-line max-md:min-h-52 max-md:border-l-0 max-md:border-t">
              <div className="flex flex-col items-center justify-center text-muted-foreground">
                <Cloud className="mb-2 size-4 text-primary" />
                <span className="text-[8px] font-semibold">
                  {t("home.connectivity.cloud")}
                </span>
                <small className="mt-1 text-[7px]">
                  {t("home.connectivity.cloudRole")}
                </small>
              </div>
              <RouteLine
                active
                label={t("home.connectivity.direct")}
                value="42 MS"
              />
              <RouteLine
                label={t("home.connectivity.relay")}
                value={t("home.connectivity.available")}
              />
            </div>
            <RouteNode
              icon={<Server />}
              label={t("home.connectivity.owner")}
              title={t("home.connectivity.daemon")}
              status={t("home.connectivity.capability")}
              last
            />
          </div>
          <div className="grid grid-cols-3 border-x border-b border-line max-md:grid-cols-1">
            <RouteFact
              icon={<LockKeyhole />}
              text={t("home.connectivity.encrypted")}
            />
            <RouteFact
              icon={<RadioTower />}
              text={t("home.connectivity.pathShown")}
            />
            <RouteFact
              icon={<ShieldCheck />}
              text={t("home.connectivity.truth")}
            />
          </div>
        </div>
      </section>

      <section
        className="border-b border-line bg-soft py-28 max-md:py-20"
        id="plans"
      >
        <div className={wrap}>
          <SectionHeading
            kicker={t("home.plans.kicker")}
            copy={t("home.plans.copy")}
          >
            {t("home.plans.title1")}
            <br />
            {t("home.plans.title2")}
          </SectionHeading>
          <div
            className="mt-16 grid min-h-[470px] grid-cols-3 border-l border-t border-line-strong max-md:mt-11 max-md:min-h-0 max-md:grid-cols-1"
            aria-live="polite"
          >
            {catalog?.catalog?.plans.map((plan) => {
              const key = `home.plans.items.${plan.planId}`;
              const features = t(`${key}.features`, {
                returnObjects: true,
              }) as string[];
              const price =
                plan.price?.mode === CatalogPriceMode.CONFIGURED
                  ? new Intl.NumberFormat(intlLocale(i18n.language), {
                      style: "currency",
                      currency: plan.price.currency,
                      maximumFractionDigits: 0,
                    }).format(Number(plan.price.monthlyMinor) / 100)
                  : t(`${key}.price`);
              const note =
                plan.price?.mode === CatalogPriceMode.CONFIGURED
                  ? t("home.plans.perMonth")
                  : plan.price?.mode === CatalogPriceMode.INCLUDED
                    ? t("home.plans.noCard")
                    : plan.planId === "pro"
                      ? t("home.plans.previewAccess")
                      : t("home.plans.custom");
              return (
                <article
                  className={cn(
                    "flex flex-col border-b border-r border-line-strong bg-panel p-7",
                    plan.presentation?.featured &&
                      "shadow-[inset_0_3px_0_var(--primary)]",
                  )}
                  key={plan.planId}
                >
                  <header className="flex justify-between gap-3 text-[8px] text-muted-foreground">
                    <span>{t(`${key}.eyebrow`)}</span>
                    {plan.presentation?.featured && (
                      <b className="text-primary">
                        {t("home.plans.recommended")}
                      </b>
                    )}
                  </header>
                  <h3 className="mt-7 text-[27px] font-light">
                    {t(`${key}.name`)}
                  </h3>
                  <p className="mt-3 min-h-16 text-[10px] leading-4 text-muted-foreground max-md:min-h-0">
                    {t(`${key}.description`)}
                  </p>
                  <div className="mt-4 flex min-h-20 flex-wrap items-baseline gap-2 border-y border-line py-5">
                    <strong className="text-[27px] font-normal">{price}</strong>
                    <small className="text-[8px] text-muted-foreground">
                      {note}
                    </small>
                  </div>
                  <ul className="my-6 flex-1 space-y-3 text-[9px] text-muted-foreground">
                    {features.map((feature) => (
                      <li className="flex gap-2 leading-4" key={feature}>
                        <Check className="size-3 shrink-0 text-success" />{" "}
                        {feature}
                      </li>
                    ))}
                  </ul>
                  <a
                    className={buttonVariants({
                      variant: plan.presentation?.featured
                        ? "default"
                        : "outline",
                    })}
                    href={plan.presentation?.ctaHref || "/login"}
                  >
                    {t(`${key}.cta`)} <ArrowRight />
                  </a>
                </article>
              );
            })}
            {!catalog && (
              <p className="col-span-full grid min-h-72 place-items-center border-b border-r border-line-strong text-[9px] text-muted-foreground">
                {t("home.plans.loading")}
              </p>
            )}
          </div>
        </div>
      </section>

      <section className="bg-inverse py-24 text-inverse-foreground max-md:py-20">
        <div
          className={`${wrap} grid grid-cols-[.6fr_1.4fr_.7fr] items-end gap-12 max-md:grid-cols-1 max-md:items-start max-md:gap-6`}
        >
          <p className="self-start text-[8px] opacity-55">
            {t("home.final.kicker")}
          </p>
          <h2 className="text-[38px] font-light leading-tight max-md:text-3xl">
            {t("home.final.title1")}
            <br />
            {t("home.final.title2")}
          </h2>
          <div className="grid gap-2">
            <a className={buttonVariants()} href="/login">
              {t("home.final.account")} <ArrowRight />
            </a>
            <a
              className={cn(
                buttonVariants({ variant: "outline" }),
                "border-white/30 bg-transparent text-white",
              )}
              href="https://github.com/lozzo/termx"
              target="_blank"
              rel="noreferrer"
            >
              {t("home.final.github")} <ExternalLink />
            </a>
          </div>
        </div>
      </section>

      <footer className="min-h-20 bg-inverse text-inverse-foreground">
        <div
          className={`${wrap} grid min-h-20 grid-cols-[1fr_auto_1fr] items-center border-t border-white/15 max-md:grid-cols-[1fr_auto]`}
        >
          <Brand href="#top" inverse />
          <a
            className="flex items-center gap-2 text-[8px] opacity-50 hover:opacity-100 max-md:hidden"
            href="https://github.com/lozzo/termx"
            target="_blank"
            rel="noreferrer"
          >
            <GitFork className="size-3" /> {t("common.openSourceCore")}
          </a>
          <span className="justify-self-end text-right text-[8px] opacity-50">
            {t("common.termxCloudOfficial")}
          </span>
        </div>
      </footer>
    </main>
  );
}

const wrap =
  "mx-auto w-[min(1200px,calc(100%_-_64px))] max-md:w-[calc(100%_-_28px)]";
function Brand({ href, inverse = false }: { href: string; inverse?: boolean }) {
  const { t } = useTranslation();
  return (
    <a
      className="flex items-center gap-2.5 justify-self-start"
      href={href}
      aria-label={t("common.home")}
    >
      <b
        className={cn(
          "grid size-8 place-items-center bg-foreground text-[10px] font-bold text-background",
          inverse && "bg-inverse-foreground text-inverse",
        )}
      >
        TX
      </b>
      <span className="grid text-[13px] font-semibold leading-tight">
        TERMX
        <small className="max-w-28 text-[7px] font-normal leading-tight text-muted-foreground">
          {t("home.brandSubtitle")}
        </small>
      </span>
    </a>
  );
}
function Kicker({ children }: { children: React.ReactNode }) {
  return (
    <p className="mb-5 flex items-center gap-2 text-[9px] font-semibold text-muted-foreground">
      <i className="h-px w-5 bg-primary" />
      {children}
    </p>
  );
}
function SectionHeading({
  kicker,
  copy,
  children,
}: {
  kicker: string;
  copy?: string;
  children: React.ReactNode;
}) {
  return (
    <header className="max-w-[820px]">
      <Kicker>{kicker}</Kicker>
      <h2 className="text-[45px] font-light leading-tight max-md:text-[33px]">
        {children}
      </h2>
      {copy && (
        <p className="mt-6 max-w-[690px] text-sm leading-6 text-muted-foreground max-md:text-[13px]">
          {copy}
        </p>
      )}
    </header>
  );
}
function TerminalRow({
  active,
  name,
  meta,
  status,
}: {
  active?: boolean;
  name: string;
  meta: string;
  status: string;
}) {
  return (
    <div
      className={cn(
        "grid min-h-20 grid-cols-[30px_1fr_auto] items-center gap-3 border-b border-line px-4 text-muted-foreground last:border-0",
        active &&
          "bg-panel text-foreground shadow-[inset_3px_0_0_var(--primary)]",
      )}
    >
      <Terminal className="size-4 text-primary" />
      <span>
        <strong className="block text-[11px] font-medium">{name}</strong>
        <small className="mt-1 block text-[7px] text-muted-foreground">
          {meta}
        </small>
      </span>
      <em className="text-[7px] not-italic text-success">{status}</em>
    </div>
  );
}
function Observer({
  icon,
  title,
  detail,
}: {
  icon: React.ReactNode;
  title: string;
  detail: string;
}) {
  return (
    <div className="flex min-h-16 items-center gap-3 border-t border-line max-lg:border-l max-lg:border-t-0 max-lg:pl-5 max-md:border-l-0 max-md:border-t max-md:pl-0">
      <span className="text-primary">{icon}</span>
      <span>
        <strong className="block text-[10px] font-medium">{title}</strong>
        <small className="mt-1 block text-[7px] text-muted-foreground">
          {detail}
        </small>
      </span>
    </div>
  );
}
function RouteNode({
  icon,
  label,
  title,
  status,
  last,
}: {
  icon: React.ReactNode;
  label: string;
  title: string;
  status: string;
  last?: boolean;
}) {
  return (
    <div
      className={cn(
        "flex min-h-44 flex-col justify-center bg-soft p-8",
        last &&
          "border-l border-line bg-panel max-md:border-l-0 max-md:border-t",
      )}
    >
      <span className="mb-8 text-primary">{icon}</span>
      <small className="text-[7px] text-muted-foreground">{label}</small>
      <strong className="mt-2 text-[13px] font-semibold">{title}</strong>
      <span className="mt-2 text-[7px] text-success">{status}</span>
    </div>
  );
}
function RouteLine({
  active,
  label,
  value,
}: {
  active?: boolean;
  label: string;
  value: string;
}) {
  return (
    <div
      className={cn(
        "grid grid-cols-[auto_1fr_auto] items-center gap-3 border-t border-line px-4 text-[7px] text-muted-foreground",
        active && "text-success",
      )}
    >
      <b className="text-[8px] font-semibold">{label}</b>
      <i className={cn("h-px bg-line-strong", active && "bg-success")} />
      <strong className="text-[8px]">{value}</strong>
    </div>
  );
}
function RouteFact({ icon, text }: { icon: React.ReactNode; text: string }) {
  return (
    <p className="m-0 flex min-h-14 items-center gap-2 border-r border-line px-4 text-[9px] text-muted-foreground last:border-0 max-md:border-b max-md:border-r-0 max-md:last:border-b-0">
      <span className="text-success">{icon}</span>
      {text}
    </p>
  );
}
