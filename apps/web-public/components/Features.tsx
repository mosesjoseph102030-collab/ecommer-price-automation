// The wireframe lists the five feature areas: Cost & margin | Competitors | Rules
// | Approvals | Reporting. Each description states what is enforced, not just what
// exists, because "reporting" alone would not tell a store owner whether the tool
// is safe to leave running unattended.
const FEATURES = [
  {
    title: "Cost & margin",
    body: "Track landed cost per product and variant with an effective-date history, so a cost change never rewrites the past. Minimum profitable price, target margin, and price ceilings are recalculated from the same numbers.",
  },
  {
    title: "Competitors",
    body: "Watch only the domains you approve. Observations are immutable and evidence-backed, matches always need a human confirmation, and a stale or currency-mismatched price is flagged rather than used.",
  },
  {
    title: "Rules",
    body: "Product, variant, category, and store rules with documented precedence, version history, and a simulator. You can see exactly which rule produced a price before it reaches WooCommerce.",
  },
  {
    title: "Approvals",
    body: "Draft, review, approve, publish, and roll back, with per-role thresholds. A price below the floor cannot be published, and a manual change made in WooCommerce becomes a conflict instead of being silently overwritten.",
  },
  {
    title: "Reporting",
    body: "Margins, competitor gaps, price-change history, publish reliability, and stock-aware opportunities. Exports are CSV with formula-injection protection on every cell.",
  },
];

export default function Features() {
  return (
    <section className="section" id="features">
      <div className="section-width">
        <h2>Product features</h2>
        <p className="section-lede">
          Five areas that cover the whole pricing decision, from cost entry to the audit trail after publishing.
        </p>
        <div className="feature-grid">
          {FEATURES.map((feature) => (
            <article key={feature.title} className="card feature">
              <h3>{feature.title}</h3>
              <p className="muted">{feature.body}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
