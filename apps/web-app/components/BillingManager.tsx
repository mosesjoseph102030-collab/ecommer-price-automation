"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type BillingStatus, type Plan, type PaymentRecord } from "@/lib/api";

const money = (kobo?: number, currency = "NGN") =>
  kobo === undefined
    ? "—"
    : new Intl.NumberFormat("en-NG", { style: "currency", currency, maximumFractionDigits: 2 }).format(kobo / 100);

const date = (value?: string) => (value ? new Date(value).toLocaleString("en-NG") : "—");

// Entitlement keys are rendered with a human label because "check_frequency_minutes"
// is meaningless to a store owner reading a plan comparison.
const ENTITLEMENT_LABELS: Record<string, string> = {
  products: "Products",
  competitors: "Monitored competitors",
  check_frequency_minutes: "Competitor check interval",
  team_members: "Team members",
  ai_requests: "AI requests / month",
  export_rows: "Export rows / month",
  publishes: "Price publishes / month",
};

const UNITS: Record<string, string> = {
  check_frequency_minutes: "min",
  ai_requests: "/mo",
  export_rows: "rows/mo",
  publishes: "/mo",
  products: "",
  competitors: "",
  team_members: "",
};

function describeLimit(key: string, value: number) {
  if (key === "check_frequency_minutes") {
    if (value >= 1440) return `every ${Math.round(value / 1440)} day`;
    if (value >= 60) return `every ${Math.round(value / 60)}h`;
    return `every ${value} min`;
  }
  return value === 0 ? "not included" : `${value.toLocaleString()}${UNITS[key] ?? ""}`;
}

const STATUS_TONE: Record<string, string> = {
  active: "badge",
  trialing: "badge",
  past_due: "alert alert-warning",
  cancelled: "badge",
  expired: "badge",
};

export default function BillingManager({ slug }: { slug: string }) {
  const [status, setStatus] = useState<BillingStatus | null>(null);
  const [plans, setPlans] = useState<Plan[]>([]);
  const [payments, setPayments] = useState<PaymentRecord[]>([]);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [working, setWorking] = useState("");
  const [cancelReason, setCancelReason] = useState("");

  const load = useCallback(async () => {
    try {
      const [statusResponse, planResponse, paymentResponse] = await Promise.all([
        api.billingStatus(slug),
        api.plans(),
        api.billingPayments(slug),
      ]);
      setStatus(statusResponse);
      setPlans(planResponse.plans);
      setPayments(paymentResponse.payments);
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not load billing information.");
    }
  }, [slug]);

  useEffect(() => {
    void load();
  }, [load]);

  // checkout sends the browser to Paystack and never returns through the SPA, so
  // the reference is stashed before redirecting and read back on return.
  async function startCheckout(planCode: string) {
    setWorking(planCode);
    setError("");
    setMessage("");
    try {
      const session = await api.startCheckout(slug, planCode);
      window.sessionStorage.setItem(`pending_paystack_reference_${slug}`, session.reference);
      window.location.href = session.authorization_url;
    } catch (cause) {
      setWorking("");
      setError(cause instanceof ApiError ? cause.message : "Could not start checkout.");
    }
  }

  async function confirmPayment() {
    setWorking("confirm");
    setError("");
    setMessage("");
    try {
      const reference = window.sessionStorage.getItem(`pending_paystack_reference_${slug}`) ?? "";
      if (!reference) {
        setError("No payment reference to confirm. If you were charged, contact support with your receipt.");
        return;
      }
      await api.confirmCheckout(slug, reference);
      window.sessionStorage.removeItem(`pending_paystack_reference_${slug}`);
      setMessage("Payment confirmed. Your plan is active.");
      await load();
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not confirm the payment.");
    } finally {
      setWorking("");
    }
  }

  async function cancel() {
    setWorking("cancel");
    setError("");
    setMessage("");
    try {
      const result = await api.cancelSubscription(slug, cancelReason);
      setCancelReason("");
      setMessage(`Subscription cancelled. ${result.data_retention_end}.`);
      await load();
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not cancel the subscription.");
    } finally {
      setWorking("");
    }
  }

  async function reactivate() {
    setWorking("reactivate");
    setError("");
    setMessage("");
    try {
      await api.reactivateSubscription(slug);
      setMessage("Subscription reactivated.");
      await load();
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not reactivate the subscription.");
    } finally {
      setWorking("");
    }
  }

  const subscription = status?.subscription ?? null;
  const pendingReference =
    typeof window !== "undefined" ? window.sessionStorage.getItem(`pending_paystack_reference_${slug}`) : null;

  return (
    <div className="stack">
      {error && <div className="alert alert-error">{error}</div>}
      {message && <div className="alert alert-success">{message}</div>}

      {status?.read_only && (
        <div className="alert alert-warning">
          <strong>This store is read-only.</strong> {status.read_only_reason}. You can still read your data and
          export it. Update payment below to restore publishing.
        </div>
      )}

      {pendingReference && (
        <div className="alert alert-warning">
          <strong>Payment not confirmed yet.</strong> If you completed payment on Paystack, verify it here so the
          server re-checks the transaction with the provider.
          <div style={{ marginTop: 10 }}>
            <button className="btn btn-primary" disabled={working === "confirm"} onClick={() => void confirmPayment()}>
              {working === "confirm" ? "Verifying…" : "Verify payment"}
            </button>
          </div>
        </div>
      )}

      <section className="panel">
        <h2>Current plan</h2>
        {!subscription ? (
          <p className="muted">No subscription yet. Choose a plan below to get started.</p>
        ) : (
          <div className="grid">
            <div>
              <span className={STATUS_TONE[subscription.status] ?? "badge"}>{subscription.status}</span>{" "}
              <strong>{subscription.plan_code}</strong>
            </div>
            <dl className="grid">
              {subscription.trial_ends_at && (
                <div>
                  <dt className="muted">Trial ends</dt>
                  <dd>{date(subscription.trial_ends_at)}</dd>
                </div>
              )}
              {subscription.current_period_end && (
                <div>
                  <dt className="muted">Current period ends</dt>
                  <dd>{date(subscription.current_period_end)}</dd>
                </div>
              )}
              {subscription.grace_ends_at && (
                <div>
                  <dt className="muted">Payment grace period ends</dt>
                  <dd>{date(subscription.grace_ends_at)}</dd>
                </div>
              )}
              {subscription.retention_ends_at && (
                <div>
                  <dt className="muted">Data retained until</dt>
                  <dd>{date(subscription.retention_ends_at)}</dd>
                </div>
              )}
            </dl>
            {subscription.cancel_at_period_end && (
              <div className="alert alert-warning">
                This subscription is set to cancel at the end of the current period. Your data is retained and you can
                reactivate at any time before then.
                <div style={{ marginTop: 10 }}>
                  <button className="btn" disabled={working === "reactivate"} onClick={() => void reactivate()}>
                    {working === "reactivate" ? "Reactivating…" : "Reactivate"}
                  </button>
                </div>
              </div>
            )}
          </div>
        )}
      </section>

      <section className="panel">
        <h2>Your allowance</h2>
        <p className="muted">Limits reset each month. Your store keeps its data regardless of plan.</p>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Limit</th>
                <th>Included</th>
                <th>Used this month</th>
              </tr>
            </thead>
            <tbody>
              {Object.keys(ENTITLEMENT_LABELS).map((key) => {
                const limit = status?.entitlements?.[key];
                const used = status?.usage?.[key] ?? 0;
                if (limit === undefined) return null;
                return (
                  <tr key={key}>
                    <td>{ENTITLEMENT_LABELS[key]}</td>
                    <td>{describeLimit(key, limit)}</td>
                    <td>{used.toLocaleString()}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>

      <section className="panel">
        <h2>Plans</h2>
        <div className="grid">
          {plans.map((plan) => {
            const current = subscription?.plan_code === plan.code;
            return (
              <div key={plan.code} className="panel" style={{ background: "var(--bg-subtle)" }}>
                <h3>
                  {plan.name} {current && <span className="badge">current</span>}
                </h3>
                <p className="muted">{plan.description}</p>
                <p style={{ fontSize: 24, fontWeight: 700, margin: "8px 0" }}>
                  {plan.price_kobo === 0 ? "Free" : money(plan.price_kobo, plan.currency)}
                  {plan.price_kobo > 0 && <span className="muted" style={{ fontSize: 14 }}> /{plan.interval === "annual" ? "year" : "month"}</span>}
                </p>
                {plan.trial_days > 0 && <p className="muted">{plan.trial_days}-day free trial</p>}
                <ul className="muted" style={{ paddingLeft: 18 }}>
                  {Object.entries(plan.entitlements).map(([key, value]) => (
                    <li key={key}>
                      {ENTITLEMENT_LABELS[key] ?? key}: {describeLimit(key, value)}
                    </li>
                  ))}
                </ul>
                {!current && plan.code !== "trial" && (
                  <button
                    className="btn btn-primary"
                    disabled={working === plan.code}
                    onClick={() => void startCheckout(plan.code)}
                  >
                    {working === plan.code ? "Redirecting…" : `Choose ${plan.name}`}
                  </button>
                )}
                {current && <p className="muted">You are on this plan.</p>}
              </div>
            );
          })}
        </div>
      </section>

      <section className="panel">
        <h2>Payment history</h2>
        {payments.length === 0 ? (
          <p className="muted">No payments recorded yet.</p>
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Date</th>
                  <th>Reference</th>
                  <th>Plan</th>
                  <th>Amount</th>
                  <th>Status</th>
                  <th>Channel</th>
                </tr>
              </thead>
              <tbody>
                {payments.map((payment) => (
                  <tr key={payment.reference}>
                    <td>{date(payment.paid_at ?? payment.created_at)}</td>
                    <td>
                      <code>{payment.reference}</code>
                    </td>
                    <td>{payment.plan_code || "—"}</td>
                    <td>{money(payment.amount_kobo, payment.currency)}</td>
                    <td>
                      <span className="badge">{payment.status}</span>
                    </td>
                    <td className="muted">{payment.channel || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="panel">
        <h2>Cancel subscription</h2>
        <p className="muted">
          Cancelling stops future billing at the end of the current period. Your products, costs, rules, and history are
          retained — nothing is deleted.
        </p>
        {subscription?.cancel_at_period_end ? (
          <p className="muted">This subscription is already scheduled to cancel.</p>
        ) : (
          <div className="field">
            <label htmlFor="cancel-reason">Why are you cancelling?</label>
            <input
              id="cancel-reason"
              value={cancelReason}
              onChange={(event) => setCancelReason(event.target.value)}
              placeholder="e.g. moving to another platform"
            />
            <button
              className="btn btn-danger"
              style={{ marginTop: 10 }}
              disabled={working === "cancel" || cancelReason.trim().length < 3}
              onClick={() => void cancel()}
            >
              {working === "cancel" ? "Cancelling…" : "Cancel subscription"}
            </button>
          </div>
        )}
      </section>
    </div>
  );
}
