"use client";

import Link from "next/link";
import { FormEvent, useState } from "react";

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const DASHBOARD_URL = process.env.NEXT_PUBLIC_DASHBOARD_URL ?? "http://localhost:3001";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_URL}/api/v1${path}`, { ...init, credentials: "include", headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) } });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error?.message ?? "Request failed.");
  return data as T;
}

export default function SigninPage() {
  const [working, setWorking] = useState(false); const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setWorking(true); setError(""); const form = new FormData(event.currentTarget);
    try {
      await request("/auth/signin", { method: "POST", body: JSON.stringify({ email: form.get("email"), password: form.get("password") }) });
      const result = await request<{ organizations: Array<{ slug: string }> }>("/orgs");
      const slug = result.organizations[0]?.slug;
      window.location.assign(slug ? `${DASHBOARD_URL}/app/${encodeURIComponent(slug)}/settings/woocommerce` : "/onboarding");
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Could not sign in."); setWorking(false); }
  }
  return <main className="auth-grid"><section><h1>Welcome back</h1><p className="muted">Sign in to manage WooCommerce connections, catalog imports, and sync health.</p></section><form className="auth-card form" onSubmit={submit}>
    {error && <div className="alert alert-error" role="alert">{error}</div>}
    <div className="field"><label htmlFor="email">Email</label><input id="email" name="email" type="email" required autoComplete="email" /></div>
    <div className="field"><label htmlFor="password">Password</label><input id="password" name="password" type="password" required autoComplete="current-password" /></div>
    <button className="button" disabled={working}>{working ? "Signing in…" : "Sign in"}</button><p>New here? <Link href="/signup">Create an account</Link></p>
  </form></main>;
}
