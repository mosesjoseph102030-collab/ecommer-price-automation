// The spec calls this a "trusted-by / proof strip" but gives no customer logos,
// and inventing brand names for a marketing page would be a false endorsement.
// So it states the three capabilities the product actually has instead, which is
// honest and still serves the section's purpose.
const PROOFS = ["Competitor tracking", "Margin protection", "Safe price publishing"];

export default function ProofStrip() {
  return (
    <section className="proof-strip" aria-label="What FinTrade does">
      <div className="section-width">
        <p className="proof-title">Built for independent WooCommerce stores</p>
        <ul className="proof-list">
          {PROOFS.map((proof) => (
            <li key={proof}>{proof}</li>
          ))}
        </ul>
      </div>
    </section>
  );
}
