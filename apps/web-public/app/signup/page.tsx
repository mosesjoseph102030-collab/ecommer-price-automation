"use client";

import Link from "next/link";
import { FormEvent, useState } from "react";

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const DASHBOARD_URL = process.env.NEXT_PUBLIC_DASHBOARD_URL ?? "http://localhost:3001";

async function call<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(`${API_URL}/api/v1${path}`, { method: "POST", credentials: "include", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error?.message ?? "Request failed.");
  return data as T;
}

export default function SignupPage() {
  const [working, setWorking] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setWorking(true); setError("");
    const form = new FormData(event.currentTarget);
    const payload = { first_name: String(form.get("first_name") ?? ""), last_name: String(form.get("last_name") ?? ""), email: String(form.get("email") ?? ""), password: String(form.get("password") ?? ""), store_name: String(form.get("store_name") ?? "") };
    try {
      await call("/auth/signup", payload);
      const org = await call<{ slug: string }>("/orgs", { name: payload.store_name, currency: "NGN", timezone: "Africa/Lagos" });
      window.location.assign(`${DASHBOARD_URL}/app/${encodeURIComponent(org.slug)}/settings/woocommerce`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not create your account.");
      setWorking(false);
    }
  }
  return <main className="auth-grid"><section><h1>Create your store workspace</h1><p className="muted">Start with secure WooCommerce catalog synchronization. Pricing automation follows in later phases.</p></section><form className="auth-card form" onSubmit={submit}>
    {error && <div className="alert alert-error" role="alert">{error}</div>}
    <div className="field"><label htmlFor="first_name">First name</label><input id="first_name" name="first_name" required autoComplete="given-name" /></div>
    <div className="field"><label htmlFor="last_name">Last name</label><input id="last_name" name="last_name" autoComplete="family-name" /></div>
    <div className="field"><label htmlFor="email">Email</label><input id="email" name="email" type="email" required autoComplete="email" /></div>
    <div className="field"><label htmlFor="password">Password</label><input id="password" name="password" type="password" minLength={8} required autoComplete="new-password" /></div>
    <div className="field"><label htmlFor="store_name">Store name</label><input id="store_name" name="store_name" required minLength={3} placeholder="Bright Tech Accessories" /></div>
    <button className="button" disabled={working}>{working ? "Creating workspace…" : "Create account"}</button><p>Already registered? <Link href="/signin">Sign in</Link></p>
  </form></main>;
}
