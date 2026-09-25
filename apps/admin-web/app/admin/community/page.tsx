"use client";

import { useCallback, useEffect, useState } from "react";

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

type Item = { report_id: string; target_type: string; target_id: string; excerpt: string; reason: string; status: string; auto_category?: string; created_at: string };

export default function ModerationPage() {
  const [items, setItems] = useState<Item[]>([]);
  const [status, setStatus] = useState("open");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setError("");
    try {
      const res = await fetch(`${API_URL}/api/v1/admin/community/moderation?status=${encodeURIComponent(status)}`, { credentials: "include" });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error?.message ?? "Could not load the moderation queue.");
      setItems(body.items);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not load the moderation queue.");
    }
  }, [status]);

  useEffect(() => {
    void load();
  }, [load]);

  async function act(id: string, action: string) {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const res = await fetch(`${API_URL}/api/v1/admin/community/reports/${id}/moderate`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action, resolution: `${action} by moderator` }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error?.message ?? "Moderation action failed.");
      setMessage(`Applied ${action}.`);
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Moderation action failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <h1>Community moderation</h1>
      <p>
        Auto-blocked content appears here with commercial figures redacted. Coordinate pricing, customer allocation,
        competitor confidentiality, and price fixing are blocked before publication.
      </p>
      {error && <div className="alert alert-error">{error}</div>}
      {message && <div className="alert alert-success">{message}</div>}

      <section className="panel">
        <div className="field">
          <label htmlFor="mod-status">Queue</label>
          <select id="mod-status" value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="open">Open</option>
            <option value="reviewing">Reviewing</option>
            <option value="actioned">Actioned</option>
            <option value="dismissed">Dismissed</option>
          </select>
        </div>
        <div style={{ overflowX: "auto" }}>
          <table>
            <thead>
              <tr>
                <th>Type</th>
                <th>Reason</th>
                <th>Auto category</th>
                <th>Reported</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {items.length === 0 && (
                <tr>
                  <td colSpan={5} className="muted">
                    Queue is empty.
                  </td>
                </tr>
              )}
              {items.map((item) => (
                <tr key={item.report_id}>
                  <td>{item.target_type}</td>
                  <td>{item.excerpt}</td>
                  <td>
                    {item.auto_category ? <span className="badge badge-warning">{item.auto_category}</span> : <span className="muted">user report</span>}
                  </td>
                  <td className="muted">{new Date(item.created_at).toLocaleString()}</td>
                  <td>
                    <div className="row">
                      <button className="btn btn-danger" disabled={busy} onClick={() => void act(item.report_id, "hide")}>
                        Hide
                      </button>
                      <button className="btn" disabled={busy} onClick={() => void act(item.report_id, "escalate")}>
                        Escalate
                      </button>
                      <button className="btn" disabled={busy} onClick={() => void act(item.report_id, "restore")}>
                        Dismiss
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}
