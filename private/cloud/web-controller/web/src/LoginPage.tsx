import { ArrowRight, Code2, ShieldCheck, UserRound } from "lucide-react";
import { FormEvent, useEffect, useState } from "react";

interface Provider {
  id: string;
  name: string;
  configured: boolean;
}
export default function LoginPage() {
  const [loading, setLoading] = useState(false),
    [error, setError] = useState(""),
    [providers, setProviders] = useState<Provider[]>([]),
    [mode, setMode] = useState<"login" | "register">("login"),
    [email, setEmail] = useState(""),
    [password, setPassword] = useState(""),
    [aff, setAff] = useState("");
  useEffect(() => {
    const saved =
      localStorage.getItem("termx-wx-theme") === "neutral-dark"
        ? "neutral-dark"
        : "light-gray";
    document.documentElement.dataset.wxTheme = saved;
    setAff(new URLSearchParams(location.search).get("aff") ?? "");
    fetch("/api/providers")
      .then((r) => r.json())
      .then(setProviders)
      .catch(() => setProviders([]));
  }, []);
  async function login() {
    setLoading(true);
    setError("");
    const r = await fetch("/api/auth/login", { method: "POST" });
    if (r.ok) location.href = "/account";
    else {
      setError("Development identity provider is unavailable.");
      setLoading(false);
    }
  }
  async function passwordAuth(event: FormEvent) {
    event.preventDefault(); setLoading(true); setError("");
    const path = mode === "register" ? "/api/auth/password/register" : "/api/auth/password/login";
    const r = await fetch(path,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({email,password,aff})});
    if(r.ok) location.href="/account"; else {const body=await r.json().catch(()=>({error:"Authentication failed"}));setError(body.error);setLoading(false)}
  }
  return (
    <main id="wx-root" className="wx-login">
      <section className="wx-login-product">
        <a className="wx-login-logo" href="/">
          <b>TX</b>
          <span>
            TERMX<small>MANAGED EDGE</small>
          </span>
        </a>
        <div className="wx-login-route" aria-label="Managed connection path">
          <span>CLIENT<small>IDENTITY VERIFIED</small></span>
          <i><b>DIRECT P2P</b><em>SINGLE RELAY / STANDBY</em></i>
          <span>DAEMON<small>TERMINAL OWNER</small></span>
        </div>
        <article>
          <p>CLOUD AVAILABLE / DIRECT / RELAY / E2E</p>
          <h1>
            Your terminal network,
            <br />
            under your control.
          </h1>
          <span>
            Reach registered machines and manage cloud services without exposing
            terminal authorization.
          </span>
          <dl>
            <div>
              <dt>TRANSPORT</dt>
              <dd>P2P</dd>
            </div>
            <div>
              <dt>FALLBACK</dt>
              <dd>RELAY</dd>
            </div>
            <div>
              <dt>SECURITY</dt>
              <dd>E2E</dd>
            </div>
          </dl>
        </article>
      </section>
      <section className="wx-login-form">
        <div>
          <a className="wx-login-mobile-logo" href="/">
            <b>TX</b> TERMX
          </a>
          <p>WEB CONTROLLER</p>
          <h2>Welcome back</h2>
          <span>
            Sign in to manage nodes, subscriptions, account settings, and referral rewards.
          </span>
          <div className="wx-providers">
            {providers.map((p) => {
              const Icon = p.id === "github" ? Code2 : UserRound;
              return (
                <button key={p.id} disabled={!p.configured}>
                  <Icon />
                  CONTINUE WITH {p.name.toUpperCase()}{" "}
                  {!p.configured && <small>UNAVAILABLE</small>}
                </button>
              );
            })}
          </div>
          <div className="wx-auth-modes"><button className={mode === "login" ? "selected" : ""} onClick={()=>setMode("login")}>SIGN IN</button><button className={mode === "register" ? "selected" : ""} onClick={()=>setMode("register")}>CREATE ACCOUNT</button></div>
          <form className="wx-auth-form" onSubmit={passwordAuth}>
            <label>EMAIL<input required type="email" autoComplete="email" value={email} onChange={e=>setEmail(e.target.value)}/></label>
            <label>PASSWORD<input required minLength={10} type="password" autoComplete={mode === "login" ? "current-password" : "new-password"} value={password} onChange={e=>setPassword(e.target.value)}/></label>
            {mode === "register" && aff && <label>AFF CODE<input value={aff} readOnly/></label>}
            <button className="wx-login-action" disabled={loading}>{loading ? "PLEASE WAIT" : mode === "login" ? "SIGN IN WITH EMAIL" : "CREATE ACCOUNT"}<ArrowRight/></button>
          </form>
          <div className="wx-divider">
            <i />
            STAGING IDENTITY
            <i />
          </div>
          <button
            className="wx-login-action"
            disabled={loading}
            onClick={login}
          >
            {loading ? "SIGNING IN" : "CONTINUE WITH STAGING ACCOUNT"}
            <ArrowRight />
          </button>
          {error && (
            <p className="wx-error" role="alert">
              {error}
            </p>
          )}
          <footer>
            <ShieldCheck />
            HTTPONLY SESSION / SAMESITE STRICT / CSRF PROTECTED
          </footer>
        </div>
      </section>
    </main>
  );
}
