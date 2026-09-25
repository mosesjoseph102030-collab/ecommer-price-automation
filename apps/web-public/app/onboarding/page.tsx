"use client";

import { FormEvent, useState } from "react";

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const DASHBOARD_URL = process.env.NEXT_PUBLIC_DASHBOARD_URL ?? "http://localhost:3001";

export default function OnboardingPage() {
  const [working, setWorking] = useState(false); const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setWorking(true); setError(""); const form = new FormData(event.currentTarget);
    try {
      const response = await fetch(`${API_URL}/api/v1/orgs`, { method: "POST", credentials: "include", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: form.get("name"), business_category: form.get("category"), currency: "NGN", timezone: "Africa/Lagos" }) });
      const data = await response.json(); if (!response.ok) throw new Error(data.error?.message ?? "Could not create store.");
      window.location.assign(`${DASHBOARD_URL}/app/${encodeURIComponent(data.slug)}/settings/woocommerce`);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Could not create store."); setWorking(false); }
  }
  return <main className="auth-grid"><section><h1>Create your store</h1><p className="muted">Your store URL and tenant boundary are created here.</p></section><form className="auth-card form" onSubmit={submit}>{error && <div className="alert alert-error">{error}</div>}<div className="field"><label htmlFor="name">Store name</label><input id="name" name="name" minLength={3} required /></div><div className="field"><label htmlFor="category">Business category</label><input id="category" name="category" placeholder="Phone accessories" /></div><button className="button" disabled={working}>{working ? "Creating…" : "Continue to WooCommerce"}</button></form></main>;
}
