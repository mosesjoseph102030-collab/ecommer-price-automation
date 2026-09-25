"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api, ApiError, type Health, type SyncRun } from "@/lib/api";

export default function WooCommercePanel({ slug }: { slug: string }) {
  const [health, setHealth] = useState<Health | null>(null);
  const [runs, setRuns] = useState<SyncRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [working, setWorking] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const load = useCallback(async () => {
    try {
      const [status, history] = await Promise.all([api.wooStatus(slug), api.syncRuns(slug)]);
      setHealth(status.health ?? null);
      setRuns(history.runs);
      setError("");
    } catch (cause) {
      setError(cause instanceof ApiError ? `${cause.message}${cause.requestId ? ` Reference: ${cause.requestId}` : ""}` : "Could not load WooCommerce status.");
    } finally { setLoading(false); }
  }, [slug]);

  useEffect(() => { void load(); }, [load]);

  useEffect(() => {
    if (!health || !["queued", "running"].includes(health.latest_sync_status)) return;
    const timer = setInterval(() => void load(), 3000);
    return () => clearInterval(timer);
  }, [health?.latest_sync_status, load]);

  async function connect(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setWorking(true); setError(""); setMessage("");
    const form = new FormData(event.currentTarget);
    try {
      await api.connectWoo(slug, {
        store_name: String(form.get("store_name") ?? ""),
        store_url: String(form.get("store_url") ?? ""),
        consumer_key: String(form.get("consumer_key") ?? ""),
        consumer_secret: String(form.get("consumer_secret") ?? ""),
      });
      event.currentTarget.reset();
      setMessage("WooCommerce connected. The first catalog sync is queued.");
      await load();
    } catch (cause) {
      setError(cause instanceof ApiError ? `${cause.message}${cause.requestId ? ` Reference: ${cause.requestId}` : ""}` : "Could not connect WooCommerce.");
    } finally { setWorking(false); }
  }

  async function action(name: "test" | "sync" | "disconnect") {
    if (name === "disconnect" && !window.confirm("Disconnect this store? Catalog data will be preserved, but no further updates can sync.")) return;
    setWorking(true); setError(""); setMessage("");
    try {
      if (name === "test") await api.testWoo(slug);
      if (name === "sync") await api.syncWoo(slug);
      if (name === "disconnect") await api.disconnectWoo(slug);
      setMessage(name === "test" ? "Connection healthy." : name === "sync" ? "Catalog sync queued." : "Store disconnected.");
      await load();
    } catch (cause) {
      setError(cause instanceof ApiError ? `${cause.message}${cause.requestId ? ` Reference: ${cause.requestId}` : ""}` : "The WooCommerce action failed.");
    } finally { setWorking(false); }
  }

  if (loading) return <div className="loading">Loading connector health…</div>;

  return (
    <div className="stack">
      {error && <div className="alert alert-error" role="alert">{error}</div>}
      {message && <div className="alert alert-success" role="status">{message}</div>}

      {health ? (
        <>
          <section className="panel" aria-labelledby="connection-heading">
            <div className="page-header">
              <div><h2 id="connection-heading">{health.connection.store_name}</h2><p className="muted">{health.connection.store_url}</p></div>
              <span className={`badge ${health.connection.status === "connected" ? "badge-success" : "badge-error"}`}>{health.connection.status}</span>
            </div>
            <div className="grid metrics">
              <div className="panel"><div className="muted">Products</div><strong>{health.catalog.products}</strong></div>
              <div className="panel"><div className="muted">Variants</div><strong>{health.catalog.variants}</strong></div>
              <div className="panel"><div className="muted">Webhook</div><strong>{health.connection.webhook_status}</strong></div>
              <div className="panel"><div className="muted">Last sync</div><strong>{health.connection.last_sync_at ? new Date(health.connection.last_sync_at).toLocaleString() : "Never"}</strong></div>
            </div>
            {health.connection.last_error && <p className="alert alert-warning">{health.connection.last_error}</p>}
            <div className="row" style={{ marginTop: 16 }}>
              <button className="btn" disabled={working} onClick={() => action("test")}>Test connection</button>
              <button className="btn btn-primary" disabled={working || health.connection.status !== "connected"} onClick={() => action("sync")}>Sync catalog</button>
              <button className="btn btn-danger" disabled={working} onClick={() => action("disconnect")}>Disconnect</button>
            </div>
          </section>

          <section className="panel" aria-labelledby="runs-heading">
            <h2 id="runs-heading">Sync history</h2>
            {runs.length === 0 ? <p className="muted">No sync runs yet.</p> : (
              <div className="table-wrap"><table><thead><tr><th>Started</th><th>Type</th><th>Status</th><th>Products</th><th>Variants</th><th>Failed</th><th>Report</th><th></th></tr></thead><tbody>
                {runs.map(run => <tr key={run.id}>
                  <td>{new Date(run.created_at).toLocaleString()}</td><td>{run.kind}</td>
                  <td><span className={`badge ${run.status === "succeeded" ? "badge-success" : run.status === "failed" ? "badge-error" : "badge-warning"}`}>{run.status}</span></td>
                  <td>{run.imported_products}</td><td>{run.imported_variants}</td><td>{run.failed_items}</td>
                  <td><a href={api.reportUrl(slug, run.id)}>CSV</a></td>
                  <td>{["failed", "partial"].includes(run.status) && <button className="btn" disabled={working} onClick={async () => { setWorking(true); try { await api.retrySync(slug, run.id); await load(); } finally { setWorking(false); } }}>Retry</button>}</td>
                </tr>)}
              </tbody></table></div>
            )}
          </section>
        </>
      ) : (
        <section className="panel">
          <h2>Connect WooCommerce</h2>
          <p className="muted">Create REST API keys with read/write permission in WooCommerce. The secret is encrypted and is never shown again.</p>
          <form className="stack" onSubmit={connect}>
            <div className="field"><label htmlFor="store_name">Store name</label><input id="store_name" name="store_name" required maxLength={100} placeholder="Bright Tech Accessories" /></div>
            <div className="field"><label htmlFor="store_url">Store URL</label><input id="store_url" name="store_url" type="url" required placeholder="https://store.example.com" /></div>
            <div className="field"><label htmlFor="consumer_key">Consumer key</label><input id="consumer_key" name="consumer_key" required autoComplete="off" /></div>
            <div className="field"><label htmlFor="consumer_secret">Consumer secret</label><input id="consumer_secret" name="consumer_secret" type="password" required autoComplete="new-password" /></div>
            <div><button className="btn btn-primary" disabled={working}>{working ? "Connecting…" : "Connect and import catalog"}</button></div>
          </form>
        </section>
      )}
    </div>
  );
}
