import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ShieldCheck } from "lucide-react";
import { useAuthInfo } from "../api/hooks";
import { api } from "../api/client";
import { ErrorBox, Spinner } from "../components/ui";
import { GitHubLink } from "../components/GitHubLink";

const errors: Record<string, string> = {
  login_failed: "Sign-in failed. Please try again.",
  not_allowed: "Your account is not allowed to access this LangDeck.",
};

export function LoginPage() {
  const info = useAuthInfo();
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const urlError = new URLSearchParams(window.location.search).get("error");

  const devLogin = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      await api("/auth/dev-login", { method: "POST", body: { email } });
      await qc.invalidateQueries();
      window.location.assign("/");
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  const passwordLogin = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      await api("/auth/password-login", { method: "POST", body: { username, password } });
      await qc.invalidateQueries();
      window.location.assign("/");
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-full items-center justify-center p-6">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center text-center">
          <img src="/logo.svg" alt="" className="mb-4 h-14 w-14 drop-shadow-[0_0_24px_rgba(168,85,247,0.45)]" />
          <h1 className="text-2xl font-semibold tracking-tight text-white">Welcome to LangDeck</h1>
          <p className="mt-1 text-sm text-ink-300">The open control plane for your LangGraph agents.</p>
        </div>
        <div className="card space-y-4 p-6">
          {urlError && <ErrorBox error={errors[urlError] ?? "Sign-in failed."} />}
          {info.isLoading && <Spinner />}
          {info.data?.mode === "oidc" && (
            <a href="/auth/login" className="btn-primary w-full">
              <ShieldCheck className="h-4 w-4" /> Continue with SSO
            </a>
          )}
          {info.data?.mode === "local" && (
            <form onSubmit={passwordLogin} className="space-y-4">
              <div>
                <label className="label" htmlFor="username">Username</label>
                <input id="username" autoComplete="username" required autoFocus className="input" value={username} onChange={(e) => setUsername(e.target.value)} />
              </div>
              <div>
                <label className="label" htmlFor="password">Password</label>
                <input id="password" type="password" autoComplete="current-password" required className="input" value={password} onChange={(e) => setPassword(e.target.value)} />
              </div>
              <ErrorBox error={err} />
              <button type="submit" className="btn-primary w-full" disabled={busy}>
                {busy ? <Spinner className="text-white" /> : "Sign in"}
              </button>
              {info.data.default_credentials && (
                <p className="text-center text-xs text-ink-400">
                  First sign-in: <code className="text-brand-300">{info.data.default_credentials}</code>
                </p>
              )}
            </form>
          )}
          {info.data?.mode === "dev" && (
            <form onSubmit={devLogin} className="space-y-4">
              <div className="rounded-xl border border-amber-400/30 bg-amber-400/5 px-3 py-2 text-xs text-amber-200">
                Development mode: any email signs in as admin. Configure OIDC before exposing LangDeck.
              </div>
              <div>
                <label className="label" htmlFor="email">Email</label>
                <input id="email" type="email" required autoFocus className="input" placeholder="you@company.com" value={email} onChange={(e) => setEmail(e.target.value)} />
              </div>
              <ErrorBox error={err} />
              <button type="submit" className="btn-primary w-full" disabled={busy}>
                {busy ? <Spinner className="text-white" /> : "Sign in"}
              </button>
            </form>
          )}
        </div>
        <div className="mt-6 flex justify-center text-sm">
          <GitHubLink label="Open source on GitHub" />
        </div>
      </div>
    </div>
  );
}
