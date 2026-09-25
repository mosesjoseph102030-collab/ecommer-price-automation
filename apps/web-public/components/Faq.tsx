// The questions are the ones a store owner actually asks before connecting a
// live catalog. Each answer states the real behaviour, including the parts that
// are inconvenient, because a FAQ that only reassures is not useful.
const FAQS = [
  {
    q: "Will FinTrade change my prices without asking?",
    a: "No. Every change starts as a draft recommendation with a stated reason and needs a human approval. You can also set per-role approval limits so a pricing manager can approve smaller moves without an owner, and larger ones are escalated.",
  },
  {
    q: "What happens to my prices if I stop paying?",
    a: "Nothing is deleted. Your store moves to read-only after a grace period: you keep reading, reporting, and exporting your data, but publishing is paused. Paying restores access immediately, and your products, costs, and history are untouched.",
  },
  {
    q: "What if I change a price directly in WooCommerce?",
    a: "FinTrade reads live state before every write. A manual change is detected and turned into a conflict for review rather than being overwritten, and a rollback will not clobber a newer manual edit.",
  },
  {
    q: "How are my WooCommerce credentials stored?",
    a: "Encrypted at rest with AES-256-GCM under a key held outside the database. Secrets are never returned to the browser, never written to logs, and never sent to the AI provider.",
  },
  {
    q: "Can I use it with more than one store?",
    a: "Yes. Each store is a separate tenant with its own catalog, costs, competitors, and audit trail, under one account.",
  },
  {
    q: "How do you handle my payment details?",
    a: "We do not see them. Checkout is completed on Paystack, and we only receive a confirmation that a payment succeeded. The card number never touches our servers.",
  },
];

export default function Faq() {
  return (
    <section className="section section-subtle" id="faq">
      <div className="section-width">
        <h2>Questions store owners ask</h2>
        <div className="faq">
          {FAQS.map((item) => (
            <details key={item.q} className="card faq-item">
              <summary>{item.q}</summary>
              <p className="muted">{item.a}</p>
            </details>
          ))}
        </div>
      </div>
    </section>
  );
}
