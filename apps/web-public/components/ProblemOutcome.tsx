// "Stop guessing your prices" is the wireframe's heading for the problem/outcome
// section. Each outcome names a concrete thing the product does rather than an
// abstract benefit, so a store owner can tell whether it applies to them.
const OUTCOMES = [
  {
    title: "Stop guessing your prices",
    body: "Landed cost is calculated from supplier, shipping, packaging, payment fees, tax, and import charges — so the price you see is the price that actually clears a margin.",
  },
  {
    title: "Know your margin before you discount",
    body: "Set a minimum profitable price and a target. FinTrade flags every product already selling below it, and tells you how much margin a proposed change costs before you approve it.",
  },
  {
    title: "Update WooCommerce without spreadsheets",
    body: "Approved changes publish straight to WooCommerce and are read back to confirm. Every write keeps a before-and-after record, and a rollback restores the last confirmed price.",
  },
];

export default function ProblemOutcome() {
  return (
    <section className="section" id="problem">
      <div className="section-width">
        <h2>Stop guessing your prices</h2>
        <div className="outcome-grid">
          {OUTCOMES.map((outcome) => (
            <article key={outcome.title} className="card outcome">
              <h3>{outcome.title}</h3>
              <p className="muted">{outcome.body}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
