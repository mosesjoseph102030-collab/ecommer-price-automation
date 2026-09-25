const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export interface PublicPlan {
  code: string;
  name: string;
  description: string;
  price_kobo: number;
  currency: string;
  interval: string;
  trial_days: number;
}

// Shown when the API cannot be reached. It matches the seeded plan catalog, and
// is labelled as indicative, because a marketing page that renders an error
// because a dependency blipped is a worse outcome than a page that shows a
// slightly stale price with an honest caveat.
const FALLBACK_PLANS: PublicPlan[] = [
  {
    code: "trial",
    name: "Free trial",
    description: "Full access while you evaluate. No card required.",
    price_kobo: 0,
    currency: "NGN",
    interval: "monthly",
    trial_days: 14,
  },
  {
    code: "starter",
    name: "Starter",
    description: "For a single store getting started with cost and margin control.",
    price_kobo: 1500000,
    currency: "NGN",
    interval: "monthly",
    trial_days: 0,
  },
  {
    code: "growth",
    name: "Growth",
    description: "For a growing store running active competitor monitoring.",
    price_kobo: 5000000,
    currency: "NGN",
    interval: "monthly",
    trial_days: 0,
  },
  {
    code: "scale",
    name: "Scale",
    description: "Higher limits and a one-hour competitor check interval.",
    price_kobo: 15000000,
    currency: "NGN",
    interval: "monthly",
    trial_days: 0,
  },
];

function formatMoney(kobo: number, currency: string) {
  if (kobo === 0) return "Free";
  return new Intl.NumberFormat("en-NG", {
    style: "currency",
    currency,
    maximumFractionDigits: 0,
  }).format(kobo / 100);
}

// Server-rendered so the pricing table is in the HTML for anyone who arrives
// without JavaScript, and revalidated hourly so a plan change reaches the page
// without a redeploy. Prices are read from the plan table rather than hardcoded,
// because a marketing page that disagrees with the checkout price is a billing
// dispute waiting to happen.
export const revalidate = 3600;

async function loadPlans(): Promise<{ plans: PublicPlan[]; live: boolean }> {
  try {
    const response = await fetch(`${API_URL}/api/v1/plans`, {
      // `next.revalidate` rather than `cache: "no-store"`: the two contradict
      // each other, and no-store would silently win and make the hourly
      // revalidation above pointless.
      next: { revalidate: 3600 },
      headers: { Accept: "application/json" },
    });
    if (!response.ok) throw new Error(`status ${response.status}`);
    const body = (await response.json()) as { plans?: PublicPlan[] };
    if (!Array.isArray(body.plans) || body.plans.length === 0) throw new Error("no plans");
    return { plans: body.plans, live: true };
  } catch {
    return { plans: FALLBACK_PLANS, live: false };
  }
}

export default async function Pricing() {
  const { plans, live } = await loadPlans();

  return (
    <section className="section" id="pricing">
      <div className="section-width">
        <h2>Pricing</h2>
        <p className="section-lede">
          Every plan includes the full pricing workflow. Higher plans raise the limits on catalog size, competitor
          checks, and AI requests.
        </p>
        {!live && (
          <p className="muted pricing-note" role="status">
            Showing indicative prices. Sign in to see the live plan for your store.
          </p>
        )}
        <div className="pricing-grid">
          {plans.map((plan) => {
            const isTrial = plan.price_kobo === 0;
            return (
              <article key={plan.code} className="card plan">
                <h3>{plan.name}</h3>
                <p className="plan-price">
                  {formatMoney(plan.price_kobo, plan.currency)}
                  {!isTrial && <span className="muted"> /{plan.interval === "annual" ? "year" : "month"}</span>}
                </p>
                <p className="muted">{plan.description}</p>
                {plan.trial_days > 0 && <p className="plan-trial">{plan.trial_days}-day free trial</p>}
                <a className="button button-primary" href="/signup">
                  {isTrial ? "Start free" : `Choose ${plan.name}`}
                </a>
              </article>
            );
          })}
        </div>
        <p className="muted pricing-footnote">
          Prices in Nigerian Naira, charged through Paystack. If a renewal fails you keep read access and your data for
          a grace period, and nothing is ever deleted for nonpayment.
        </p>
      </div>
    </section>
  );
}
