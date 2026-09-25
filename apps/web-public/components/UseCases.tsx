// The wireframe calls this "Testimonials or use cases". Use cases are used rather
// than testimonials because there is no real customer testimony to publish, and
// inventing quotes attributed to named store owners would be a fabricated
// endorsement. These are scenarios, clearly labelled as such.
const USE_CASES = [
  {
    audience: "Fashion and accessories",
    problem: "Seasonal markdowns are set from memory, and the cost of a changed supplier is not reflected until the next spreadsheet.",
    outcome: "A margin rule per category holds the floor while markdowns are allowed inside a defined band.",
  },
  {
    audience: "Electronics resellers",
    problem: "Prices move daily, and a manual change made in WooCommerce is overwritten by an automated publish.",
    outcome: "Live read-back turns a manual change into a conflict to review instead of a silent overwrite.",
  },
  {
    audience: "Home and solar retail",
    problem: "Landed cost includes shipping, tax, and import charges that are not in the WooCommerce product record.",
    outcome: "True landed cost is calculated once and reused for margin, floor, and competitor-gap reporting.",
  },
  {
    audience: "Multi-store operators",
    problem: "A pricing mistake in one store is discovered by a customer rather than by a check.",
    outcome: "Read-only mode after a failed payment, an approval trail per store, and a global kill switch.",
  },
];

export default function UseCases() {
  return (
    <section className="section section-subtle" id="use-cases">
      <div className="section-width">
        <h2>Where it helps</h2>
        <p className="section-lede">
          Scenarios FinTrade is built for. These are illustrative use cases, not customer testimonials.
        </p>
        <div className="usecase-grid">
          {USE_CASES.map((useCase) => (
            <article key={useCase.audience} className="card usecase">
              <h3>{useCase.audience}</h3>
              <dl>
                <dt className="muted">Situation</dt>
                <dd>{useCase.problem}</dd>
                <dt className="muted">What FinTrade does</dt>
                <dd>{useCase.outcome}</dd>
              </dl>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
