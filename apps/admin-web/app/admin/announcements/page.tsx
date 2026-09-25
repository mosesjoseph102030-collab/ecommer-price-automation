"use client";

import { useCallback, useEffect, useState } from "react";

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

type Announcement = {
  id: string; title: string; body_text: string; body_html: string; status: string;
  severity: string; publish_at?: string; published_at?: string; current_version: number; created_at: string;
  audience?: Record<string, unknown>;
};

const emptyForm = { title: "", body_text: "", body_html: "", severity: "info", status: "draft", publish_at: "", plan_codes: "", beta_access: false, connected_only: false };

export default function AnnouncementsPage() {
  const [rows, setRows] = useState<Announcement[]>([]);
  const [form, setForm] = useState(emptyForm);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetch(`${API_URL}/api/v1/admin/announcements`, { credentials: "include" });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error?.message ?? "Could not load announcements.");
      setRows(body.announcements);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not load announcements.");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function publish(scheduled: boolean) {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const res = await fetch(`${API_URL}/api/v1/admin/announcements`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          title: form.title,
          body_text: form.body_text,
          body_html: form.body_html,
          severity: form.severity,
          status: scheduled ? "scheduled" : "published",
          publish_at: scheduled && form.publish_at ? new Date(form.publish_at).toISOString() : null,
          audience: {
            plan_codes: form.plan_codes ? form.plan_codes.split(",").map((s) => s.trim()).filter(Boolean) : [],
            beta_access: form.beta_access,
            connected_only: form.connected_only,
          },
        }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error?.message ?? "Could not create the announcement.");
      setForm(emptyForm);
      setMessage(scheduled ? "Scheduled." : "Published to the resolved audience.");
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not create the announcement.");
    } finally {
      setBusy(false);
    }
  }

  async function archive(id: string) {
    setBusy(true);
    try {
      const res = await fetch(`${API_URL}/api/v1/admin/announcements/${id}/archive`, { method: "POST", credentials: "include" });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error?.message ?? "Could not archive.");
      setMessage("Archived.");
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not archive.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <h1>Announcements</h1>
      <p>Target by plan, beta access, or connection status. Rich content is sanitized; only a small formatting allowlist survives.</p>
      {error && <div className="alert alert-error">{error}</div>}
      {message && <div className="alert alert-success">{message}</div>}

      <section className="panel">
        <h2>New announcement</h2>
        <div className="field">
          <label htmlFor="an-title">Title</label>
          <input id="an-title" value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} />
        </div>
        <div className="field">
          <label htmlFor="an-body">Body (plain text)</label>
          <textarea id="an-body" rows={5} value={form.body_text} onChange={(e) => setForm({ ...form, body_text: e.target.value })} />
        </div>
        <div className="field">
          <label htmlFor="an-html">Body (rich, optional)</label>
          <textarea id="an-html" rows={3} value={form.body_html} onChange={(e) => setForm({ ...form, body_html: e.target.value })} placeholder="&lt;p&gt;Allowed&lt;/p&gt;" />
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="an-sev">Severity</label>
            <select id="an-sev" value={form.severity} onChange={(e) => setForm({ ...form, severity: e.target.value })}>
              <option value="info">Info</option>
              <option value="critical">Critical incident</option>
            </select>
          </div>
          <div className="field">
            <label htmlFor="an-plans">Plan codes (comma separated, blank = all)</label>
            <input id="an-plans" value={form.plan_codes} onChange={(e) => setForm({ ...form, plan_codes: e.target.value })} />
          </div>
          <div className="field">
            <label htmlFor="an-when">Schedule for (optional)</label>
            <input id="an-when" type="datetime-local" value={form.publish_at} onChange={(e) => setForm({ ...form, publish_at: e.target.value })} />
          </div>
        </div>
        <div className="row">
          <label>
            <input type="checkbox" checked={form.beta_access} onChange={(e) => setForm({ ...form, beta_access: e.target.checked })} /> Beta access only
          </label>
          <label>
            <input type="checkbox" checked={form.connected_only} onChange={(e) => setForm({ ...form, connected_only: e.target.checked })} /> Connected stores only
          </label>
        </div>
        <div className="row">
          <button className="btn btn-primary" disabled={busy || form.title.length === 0 || form.body_text.length === 0} onClick={() => void publish(false)}>
            Publish now
          </button>
          <button className="btn" disabled={busy || form.title.length === 0 || form.body_text.length === 0 || form.publish_at === ""} onClick={() => void publish(true)}>
            Schedule
          </button>
        </div>
      </section>

      <section className="panel">
        <div style={{ overflowX: "auto" }}>
          <table>
            <thead>
              <tr>
                <th>Title</th>
                <th>Status</th>
                <th>Severity</th>
                <th>Version</th>
                <th>Publish time</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id}>
                  <td>{row.title}</td>
                  <td>
                    <span className="badge">{row.status}</span>
                  </td>
                  <td>{row.severity}</td>
                  <td>v{row.current_version}</td>
                  <td>{row.published_at ? new Date(row.published_at).toLocaleString() : row.publish_at ? `scheduled ${new Date(row.publish_at).toLocaleString()}` : "—"}</td>
                  <td>
                    {row.status !== "archived" && (
                      <button className="btn" disabled={busy} onClick={() => void archive(row.id)}>
                        Archive
                      </button>
                    )}
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
