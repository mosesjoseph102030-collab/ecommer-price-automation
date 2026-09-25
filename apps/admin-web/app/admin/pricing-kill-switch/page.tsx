"use client";

import { useCallback, useEffect, useState } from "react";

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

type KillSwitch = { scope: string; enabled: boolean; reason: string; updated_at: string };

export default function PlatformKillSwitchPage() {
  const [state, setState] = useState<KillSwitch | null>(null);
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetch(`${API_URL}/api/v1/admin/integrations/pricing-kill-switch`, { credentials: "include" });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error?.message ?? "Could not load kill switch.");
      setState(body);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not load kill switch.");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function toggle() {
    if (!state) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const res = await fetch(`${API_URL}/api/v1/admin/integrations/pricing-kill-switch`, {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled: !state.enabled, reason: reason || "Platform admin action" }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error?.message ?? "Could not update kill switch.");
      setState(body);
      setReason("");
      setMessage(state.enabled ? "Publishing re-enabled platform-wide." : "Publishing disabled platform-wide.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not update kill switch.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <h1>Platform pricing kill switch</h1>
      <p>Stops all automated price publishing across every tenant. It cancels queued jobs; it never reverts prices that were already published.</p>
      {error && <div className="alert alert-error">{error}</div>}
      {message && <div className="alert alert-success">{message}</div>}
      <section className="panel">
        <p>
          Current state: <strong>{state ? (state.enabled ? "ENABLED" : "disabled") : "unknown"}</strong>
        </p>
        {state?.reason && <p className="muted">Reason: {state.reason}</p>}
        {state?.updated_at && <p className="muted">Updated: {new Date(state.updated_at).toLocaleString()}</p>}
        <div className="field">
          <label htmlFor="kill-reason">Reason (required)</label>
          <input id="kill-reason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Incident INC-42" />
        </div>
        <button className={state?.enabled ? "btn" : "btn btn-danger"} disabled={busy || reason.length < 3} onClick={() => void toggle()}>
          {state?.enabled ? "Re-enable publishing" : "Disable all publishing"}
        </button>
      </section>
    </>
  );
}
