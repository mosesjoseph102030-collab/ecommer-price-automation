"use client";

import { useEffect, useState } from "react";
import { api, type Health } from "@/lib/api";

export default function OverviewPanel({ slug }: { slug: string }) {
  const [health, setHealth] = useState<Health | null>(null);
  const [error, setError] = useState("");
  useEffect(() => { api.wooStatus(slug).then(v => setHealth(v.health ?? null)).catch(() => setError("Could not load connector health.")); }, [slug]);
  if (error) return <div className="alert alert-error">{error}</div>;
  if (!health) return <section className="panel"><h2>Connect WooCommerce</h2><p>Import products and variants before configuring costs and pricing in the next phase.</p><a className="btn btn-primary" href={`/app/${slug}/settings/woocommerce`}>Connect store</a></section>;
  return <section className="panel"><div className="page-header"><div><h2>Catalog connection</h2><p className="muted">{health.connection.store_name} · {health.connection.status}</p></div><a className="btn" href={`/app/${slug}/settings/woocommerce`}>Manage connection</a></div><div className="grid metrics"><div><span className="muted">Products</span><h3>{health.catalog.products}</h3></div><div><span className="muted">Variants</span><h3>{health.catalog.variants}</h3></div><div><span className="muted">Webhook events</span><h3>{health.webhook_events}</h3></div></div></section>;
}
