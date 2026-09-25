"use client";

import { useCallback, useEffect, useState } from "react";
import {
  api,
  ApiError,
  type Recommendation,
  type PriceChangeRequest,
  type KillSwitch,
  type ApprovalLimit,
} from "@/lib/api";

const naira = (k?: number) =>
  k === undefined ? "—" : new Intl.NumberFormat("en-NG", { style: "currency", currency: "NGN" }).format(k / 100);

const percent = (bps: number) => `${(bps / 100).toFixed(1)}%`;

// Guard against a zero current price (possible for a product with no price yet).
const changePercent = (from: number, to: number) =>
  from > 0 ? percent(Math.round(((to - from) / from) * 10000)) : "—";

function errText(cause: unknown, fallback: string) {
  return cause instanceof ApiError ? cause.message : fallback;
}

const STATE_LABEL: Record<string, string> = {
  hold: "Hold",
  raise: "Raise",
  lower: "Lower",
  investigate: "Investigate",
  pause: "Paused",
};

export default function RecommendationManager({ slug }: { slug: string }) {
  const [recommendations, setRecommendations] = useState<Recommendation[]>([]);
  const [requests, setRequests] = useState<PriceChangeRequest[]>([]);
  const [killSwitch, setKillSwitch] = useState<KillSwitch | null>(null);
  const [limits, setLimits] = useState<ApprovalLimit[]>([]);
  const [stateFilter, setStateFilter] = useState("");
  const [urgencyFilter, setUrgencyFilter] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [working, setWorking] = useState(false);

  const load = useCallback(async () => {
    try {
      const params = new URLSearchParams();
      if (stateFilter) params.set("state", stateFilter);
      if (urgencyFilter) params.set("urgency", urgencyFilter);
      const query = params.toString();
      const [recs, changes, kill, roleLimits] = await Promise.all([
        api.recommendations(slug, query ? `?${query}` : ""),
        api.priceChanges(slug),
        api.killSwitch(slug),
        api.approvalLimits(slug),
      ]);
      setRecommendations(recs.recommendations);
      setRequests(changes.requests);
      setKillSwitch(kill);
      setLimits(roleLimits.limits);
    } catch (cause) {
      setError(errText(cause, "Could not load recommendations."));
    }
  }, [slug, stateFilter, urgencyFilter]);

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
      setError(errText(cause, "Action failed."));
    } finally {
      setWorking(false);
    }
  }

  const pending = requests.filter((r) => r.status === "pending");
  const inFlight = requests.filter((r) => r.status === "approved" || r.status === "published" || r.status === "rolled_back");

  return (
    <div className="stack">
      <section className="panel">
        <div className="row between">
          <div>
            <h2>Price recommendations</h2>
            <p className="muted">
              Deterministic suggestions from cost, margin, and confirmed competitor data. Nothing is published without
              approval, and no price can go below the safe floor.
            </p>
          </div>
          <button className="btn" disabled={working} onClick={() => void run(() => api.generateRecommendations(slug), "Recommendations recomputed.")}>
            Recompute now
          </button>
        </div>
      </section>

      {killSwitch?.enabled && (
        <div className="alert alert-error">
          <strong>Publishing is disabled.</strong> {killSwitch.reason} (scope: {killSwitch.scope})
        </div>
      )}
      {error && <div className="alert alert-error">{error}</div>}
      {message && <div className="alert alert-success">{message}</div>}

      <section className="panel">
        <div className="row">
          <div className="field">
            <label htmlFor="rec-state">State</label>
            <select id="rec-state" value={stateFilter} onChange={(e) => setStateFilter(e.target.value)}>
              <option value="">All</option>
              {Object.entries(STATE_LABEL).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="rec-urgency">Urgency</label>
            <select id="rec-urgency" value={urgencyFilter} onChange={(e) => setUrgencyFilter(e.target.value)}>
              <option value="">All</option>
              <option value="high">High</option>
            </select>
          </div>
        </div>
      </section>

      <section className="panel table-wrap">
        <h3>Recommendation inbox</h3>
        <table>
          <thead>
            <tr>
              <th>Product</th>
              <th>State</th>
              <th>Current</th>
              <th>Recommended</th>
              <th>Change</th>
              <th>Lowest competitor</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {recommendations.length === 0 && (
              <tr>
                <td colSpan={7} className="muted">
                  No recommendations yet. Add a landed cost and recompute.
                </td>
              </tr>
            )}
            {recommendations.map((rec) => {
              const expired = new Date(rec.expires_at).getTime() < Date.now();
              const actionable = (rec.state === "raise" || rec.state === "lower") && !expired;
              return (
                <tr key={rec.id}>
                  <td>
                    <strong>{rec.product_name}</strong>
                    <div className="muted">
                      {rec.product_sku}
                      {rec.variant_id ? " · variant" : ""}
                    </div>
                  </td>
                  <td>
                    <span className="badge">{STATE_LABEL[rec.state] ?? rec.state}</span>
                    {rec.margin_risk !== "none" && <div className="muted">margin risk: {rec.margin_risk}</div>}
                    {rec.urgency === "high" && <div className="muted">high urgency</div>}
                  </td>
                  <td>{naira(rec.previous_price_kobo)}</td>
                  <td>
                    {naira(rec.recommended_price_kobo)}
                    {rec.opportunity_kobo ? <div className="muted">+{naira(rec.opportunity_kobo)} opportunity</div> : null}
                  </td>
                  <td>{percent(rec.change_bps)}</td>
                  <td>{naira(rec.lowest_confirmed_competitor_kobo)}</td>
                  <td>
                    <div className="row">
                      <button className="btn" onClick={() => setExpanded(expanded === rec.id ? null : rec.id)}>
                        {expanded === rec.id ? "Hide" : "Why?"}
                      </button>
                      {actionable && (
                        <button
                          className="btn btn-primary"
                          disabled={working}
                          onClick={() => void run(() => api.submitRecommendation(slug, rec.id), "Submitted for approval.")}
                        >
                          Submit for approval
                        </button>
                      )}
                    </div>
                    {expanded === rec.id && (
                      <div className="muted">
                        <p>{rec.explanation}</p>
                        <ul>
                          {rec.reasons.map((reason, index) => (
                            <li key={index}>
                              {reason.message}
                              {reason.amount_kobo ? ` (${naira(reason.amount_kobo)})` : ""}
                            </li>
                          ))}
                        </ul>
                        <p>
                          Confidence {percent(rec.confidence_bps)} · floor {naira(rec.minimum_profitable_price_kobo)}
                          {rec.maximum_price_kobo ? ` · ceiling ${naira(rec.maximum_price_kobo)}` : ""}
                          {expired ? " · EXPIRED" : ""}
                        </p>
                      </div>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </section>

      <section className="panel table-wrap">
        <h3>Awaiting approval ({pending.length})</h3>
        <table>
          <thead>
            <tr>
              <th>Product</th>
              <th>Change</th>
              <th>Status</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {pending.length === 0 && (
              <tr>
                <td colSpan={4} className="muted">
                  Nothing waiting for approval.
                </td>
              </tr>
            )}
            {pending.map((request) => (
              <tr key={request.id}>
                <td>
                  <strong>{request.product_name}</strong>
                  <div className="muted">
                    {naira(request.previous_price_kobo)} → {naira(request.requested_price_kobo)}
                  </div>
                </td>
                <td>{changePercent(request.previous_price_kobo, request.requested_price_kobo)}</td>
                <td>
                  <span className="badge">{request.status}</span>
                </td>
                <td>
                  <div className="row">
                    <button
                      className="btn btn-primary"
                      disabled={working}
                      onClick={() => void run(() => api.approvePriceChange(slug, request.id, "approved"), "Approved.")}
                    >
                      Approve
                    </button>
                    <button
                      className="btn btn-danger"
                      disabled={working}
                      onClick={() => void run(() => api.approvePriceChange(slug, request.id, "rejected"), "Rejected.")}
                    >
                      Reject
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <section className="panel table-wrap">
        <h3>Approved and published</h3>
        <table>
          <thead>
            <tr>
              <th>Product</th>
              <th>Change</th>
              <th>Status</th>
              <th>Scheduled</th>
              <th>Verified</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {inFlight.length === 0 && (
              <tr>
                <td colSpan={6} className="muted">
                  No approved changes yet.
                </td>
              </tr>
            )}
            {inFlight.map((request) => (
              <tr key={request.id}>
                <td>
                  <strong>{request.product_name}</strong>
                  <div className="muted">
                    {naira(request.previous_price_kobo)} → {naira(request.requested_price_kobo)}
                  </div>
                </td>
                <td>{changePercent(request.previous_price_kobo, request.requested_price_kobo)}</td>
                <td>
                  <span className="badge">{request.status}</span>
                  {request.execution_status && <div className="muted">{request.execution_status}</div>}
                  {request.last_error && <div className="muted">{request.last_error}</div>}
                </td>
                <td>{request.scheduled_for ? new Date(request.scheduled_for).toLocaleString() : "Immediate"}</td>
                <td>{naira(request.verified_after_price_kobo)}</td>
                <td>
                  <div className="row">
                    {request.status === "approved" && (
                      <button
                        className="btn btn-primary"
                        disabled={working || killSwitch?.enabled}
                        onClick={() => void run(() => api.publishPriceChange(slug, request.id), "Publish queued.")}
                      >
                        Publish now
                      </button>
                    )}
                    {request.status === "published" && request.execution_id && (
                      <button
                        className="btn btn-danger"
                        disabled={working || killSwitch?.enabled}
                        onClick={() => void run(() => api.rollbackPriceChange(slug, request.execution_id!), "Rollback queued.")}
                      >
                        Roll back
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <section className="panel">
        <h3>Safety controls</h3>
        <div className="row between">
          <div>
            <p>
              Kill switch: <strong>{killSwitch?.enabled ? "ON" : "OFF"}</strong>
              {killSwitch?.reason ? ` — ${killSwitch.reason}` : ""}
            </p>
            <p className="muted">Turning it on cancels queued publishes. Nothing already published is reverted automatically.</p>
          </div>
          <button
            className={killSwitch?.enabled ? "btn" : "btn btn-danger"}
            disabled={working}
            onClick={() =>
              void run(
                () => api.setKillSwitch(slug, !killSwitch?.enabled, killSwitch?.enabled ? "Re-enabled by store owner" : "Disabled by store owner"),
                killSwitch?.enabled ? "Publishing re-enabled." : "Publishing disabled.",
              )
            }
          >
            {killSwitch?.enabled ? "Re-enable publishing" : "Disable publishing"}
          </button>
        </div>
        <h4>Role approval limits</h4>
        <ul>
          {limits.map((limit) => (
            <li key={limit.role_id}>
              {limit.role_id}: up to {percent(limit.maximum_change_bps)} per change
              {limit.maximum_price_kobo ? `, max ${naira(limit.maximum_price_kobo)}` : ""}
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
