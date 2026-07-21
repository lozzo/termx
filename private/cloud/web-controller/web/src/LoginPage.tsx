import { create } from "@bufbuild/protobuf";
import { ArrowRight, ShieldCheck } from "lucide-react";
import { FormEvent, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  PasswordLoginRequestSchema,
  PasswordLoginResponseSchema,
  RegisterAccountRequestSchema,
  RegisterAccountResponseSchema,
} from "@/generated/cloudpb/cloud_product_pb";
import { protoPost } from "@/protoApi";

export default function LoginPage() {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (mode === "login") {
        await protoPost(
          "/api/v1/account/login",
          PasswordLoginRequestSchema,
          create(PasswordLoginRequestSchema, { email, password }),
          PasswordLoginResponseSchema,
        );
      } else {
        await protoPost(
          "/api/v1/account/register",
          RegisterAccountRequestSchema,
          create(RegisterAccountRequestSchema, { email, password }),
          RegisterAccountResponseSchema,
        );
      }
      location.href = "/account";
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Authentication failed",
      );
      setBusy(false);
    }
  }

  return (
    <main className="grid min-h-dvh bg-background text-foreground lg:grid-cols-[minmax(0,1fr)_480px]">
      <section className="hidden border-r border-line bg-inverse p-12 text-inverse-foreground lg:flex lg:flex-col lg:justify-between">
        <a className="flex items-center gap-3" href="/">
          <b className="grid size-9 place-items-center bg-primary font-mono text-xs">
            TX
          </b>
          <span>Muxvia Cloud</span>
        </a>
        <div className="max-w-2xl">
          <p className="font-mono text-xs text-success">
            ACCOUNT / CONNECTION CONTROL
          </p>
          <h1 className="mt-5 text-5xl font-light leading-tight">
            Manage connectivity without moving terminal trust into the cloud.
          </h1>
          <p className="mt-6 max-w-xl text-sm leading-7 text-[#b8bec7]">
            The Controller stores account, subscription, quota and topology
            metadata. Terminal grants and payload remain end to end between your
            devices.
          </p>
        </div>
        <p className="font-mono text-[10px] text-[#929aa6]">
          HttpOnly Cookie / SameSite Strict / CSRF
        </p>
      </section>
      <section className="flex items-center px-6 py-12 sm:px-12">
        <div className="mx-auto w-full max-w-sm">
          <p className="font-mono text-[10px] text-primary">
            MUXVIA CLOUD CONTROLLER
          </p>
          <h2 className="mt-4 text-4xl font-light">
            {mode === "login" ? "Sign in" : "Create account"}
          </h2>
          <div className="mt-8 grid grid-cols-2 border border-line">
            <button
              className={`h-11 border-b-2 text-xs ${mode === "login" ? "border-primary bg-panel" : "border-transparent text-muted-foreground"}`}
              onClick={() => setMode("login")}
            >
              Sign in
            </button>
            <button
              className={`h-11 border-b-2 text-xs ${mode === "register" ? "border-primary bg-panel" : "border-transparent text-muted-foreground"}`}
              onClick={() => setMode("register")}
            >
              Register
            </button>
          </div>
          <form className="mt-6 grid gap-4" onSubmit={submit}>
            <label className="grid gap-2 font-mono text-[10px] text-muted-foreground">
              EMAIL
              <Input
                data-testid="account-email"
                required
                type="email"
                autoComplete="email"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
              />
            </label>
            <label className="grid gap-2 font-mono text-[10px] text-muted-foreground">
              PASSWORD
              <Input
                data-testid="account-password"
                required
                minLength={8}
                type="password"
                autoComplete={
                  mode === "login" ? "current-password" : "new-password"
                }
                value={password}
                onChange={(event) => setPassword(event.target.value)}
              />
            </label>
            <Button
              data-testid="account-submit"
              className="mt-2 justify-between"
              disabled={busy}
            >
              {busy
                ? "Please wait"
                : mode === "login"
                  ? "Sign in"
                  : "Create account"}
              <ArrowRight />
            </Button>
          </form>
          {error && (
            <p
              className="mt-4 border border-destructive p-3 text-xs text-destructive"
              role="alert"
            >
              {error}
            </p>
          )}
          <div className="mt-8 flex items-center gap-3 border-t border-line pt-5 text-xs text-muted-foreground">
            <ShieldCheck className="size-4" />
            Credentials are never returned to page JavaScript.
          </div>
        </div>
      </section>
    </main>
  );
}
