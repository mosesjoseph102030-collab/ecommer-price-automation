"use client";

import { useEffect, useState } from "react";

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
type Row = { organization_id: string; store_name: string; store_url: string; status: string; webhook_status: string; last_sync_at?: string; last_error: string; rate_limited_until?: string };

export default function IntegrationsPage() {
  const [rows, setRows] = useState<Row[]>([]); const [error, setError] = useState(""); const [loading, setLoading] = useState(true);
  useEffect(() => { fetch(`${API_URL}/api/v1/admin/integrations/woocommerce`, { credentials: "include" }).then(async r => { const data = await r.json(); if (!r.ok) throw new Error(data.error?.message ?? "Could not load diagnostics."); setRows(data.connections); }).catch(e => setError(e instanceof Error ? e.message : "Could not load diagnostics.")).finally(() => setLoading(false)); }, []);
  return <><h1>WooCommerce connector health</h1>{error && <div className="alert">{error}</div>}<section className="panel">{loading ? <p>Loading…</p> : <div style={{ overflowX: "auto" }}><table><thead><tr><th>Store</th><th>URL</th><th>API</th><th>Webhook</th><th>Last sync</th><th>Error</th><th>Rate limit resets</th></tr></thead><tbody>{rows.map(row => <tr key={row.organization_id + row.store_url}><td>{row.store_name}</td><td>{row.store_url}</td><td><span className={`badge ${row.status === "connected" ? "success" : "error"}`}>{row.status}</span></td><td>{row.webhook_status}</td><td>{row.last_sync_at ? new Date(row.last_sync_at).toLocaleString() : "Never"}</td><td>{row.last_error || "—"}</td><td>{row.rate_limited_until ? new Date(row.rate_limited_until).toLocaleString() : "—"}</td></tr>)}</tbody></table></div>}</section></>;
}
