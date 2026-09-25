"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type NotificationItem, type NotificationConsent } from "@/lib/api";

const CRITICAL_TYPES = ["price_change.conflict", "price_change.failed", "announcement.critical", "security.credential_expired"];

export default function NotificationManager({ slug }: { slug: string }) {
  const [items, setItems] = useState<NotificationItem[]>([]);
  const [unread, setUnread] = useState(0);
  const [consents, setConsents] = useState<NotificationConsent[]>([]);
  const [whatsapp, setWhatsapp] = useState("");
  const [email, setEmail] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [working, setWorking] = useState(false);

  const load = useCallback(async () => {
    try {
      const [list, consentList] = await Promise.all([api.notifications(slug), api.notificationConsents(slug)]);
      setItems(list.notifications);
      setUnread(list.unread);
      setConsents(consentList.consents);
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not load notifications.");
    }
  }, [slug]);

  useEffect(() => {
    void load();
  }, [load]);

  async function run(action: () => Promise<unknown>, success: string) {
    setWorking(true);
    setError("");
    setMessage("");
    try {
      await action();
      setMessage(success);
      await load();
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Action failed.");
    } finally {
      setWorking(false);
    }
  }

  const consentFor = (channel: string) => consents.find((c) => c.channel === channel);

  return (
    <div className="stack">
      <section className="panel">
        <h2>Notification centre</h2>
        <p className="muted">
          {unread} unread. Delivery failures stay visible so a broken channel never silently swallows an alert.
        </p>
      </section>

      {error && <div className="alert alert-error">{error}</div>}
      {message && <div className="alert alert-success">{message}</div>}

      <section className="panel">
        <h3>Channel consent</h3>
        <p className="muted">WhatsApp and email are only used after you grant consent with a verified destination.</p>
        <table>
          <thead>
            <tr>
              <th>Channel</th>
              <th>Status</th>
              <th>Destination</th>
            </tr>
          </thead>
          <tbody>
            {consents.length === 0 && (
              <tr>
                <td colSpan={3} className="muted">
                  No outbound channel granted yet. In-app is always on.
                </td>
              </tr>
            )}
            {consents.map((consent) => (
              <tr key={consent.channel}>
                <td>{consent.channel}</td>
                <td>
                  <span className={`badge ${consent.status === "granted" ? "badge-success" : ""}`}>{consent.status}</span>
                </td>
                <td className="muted">{consent.destination || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="row">
          <div className="field">
            <label htmlFor="wa">WhatsApp (E.164)</label>
            <input id="wa" value={whatsapp} onChange={(e) => setWhatsapp(e.target.value)} placeholder="+2348012345678" />
          </div>
          <button
            className="btn"
            disabled={working || whatsapp.length < 10}
            onClick={() => void run(() => api.setNotificationConsent(slug, "whatsapp", "granted", whatsapp), "WhatsApp consent granted.")}
          >
            Grant WhatsApp
          </button>
          <button className="btn" disabled={working} onClick={() => void run(() => api.setNotificationConsent(slug, "whatsapp", "revoked"), "WhatsApp consent revoked.")}>
            Revoke WhatsApp
          </button>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="em">Email</label>
            <input id="em" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="owner@example.com" />
          </div>
          <button
            className="btn"
            disabled={working || email.length < 3}
            onClick={() => void run(() => api.setNotificationConsent(slug, "email", "granted", email), "Email consent granted.")}
          >
            Grant email
          </button>
          <button className="btn" disabled={working} onClick={() => void run(() => api.setNotificationConsent(slug, "email", "revoked"), "Email consent revoked.")}>
            Revoke email
          </button>
        </div>
      </section>

      <section className="panel">
        <h3>Alert preferences</h3>
        <p className="muted">
          Critical alerts ({CRITICAL_TYPES.join(", ")}) always reach the in-app centre. Muting a critical alert requires an
          explicit override reason.
        </p>
      </section>

      <section className="panel table-wrap">
        <h3>Notifications</h3>
        <table>
          <thead>
            <tr>
              <th>Alert</th>
              <th>When</th>
              <th>Delivery</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {items.length === 0 && (
              <tr>
                <td colSpan={4} className="muted">
                  Nothing here yet.
                </td>
              </tr>
            )}
            {items.map((item) => (
              <tr key={item.id}>
                <td>
                  <strong>{item.title}</strong>
                  {!item.read && <span className="badge"> new</span>}
                  <div className="muted">{item.body}</div>
                </td>
                <td className="muted">{new Date(item.created_at).toLocaleString()}</td>
                <td>
                  {(item.deliveries ?? []).map((d) => (
                    <div key={d.id}>
                      <span className={`badge ${d.status === "failed" ? "badge-error" : d.status === "sent" ? "badge-success" : ""}`}>
                        {d.channel} {d.status}
                      </span>
                      {d.last_error && <span className="muted"> {d.last_error}</span>}
                    </div>
                  ))}
                </td>
                <td>
                  {!item.read && (
                    <button className="btn" disabled={working} onClick={() => void run(() => api.markNotificationRead(slug, item.id), "Marked as read.")}>
                      Mark read
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  );
}
