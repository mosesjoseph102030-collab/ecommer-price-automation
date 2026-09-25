// The four steps are the wireframe's, verbatim. The detail lines underneath each
// step are not decoration: they name the real feature that implements the step, so
// the section cannot drift away from what the product does.
const STEPS = [
  {
    title: "Connect store",
    detail: "Authorize the WooCommerce REST API. Credentials are encrypted at rest and never returned to the browser.",
  },
  {
    title: "Add costs and competitor links",
    detail: "Enter costs by hand or import a CSV, then track competitor prices on domains you approve.",
  },
  {
    title: "Review safe recommendations",
    detail: "Each suggestion carries its reason — cost, margin, competitor, stock, rule, or guardrail — before you approve it.",
  },
  {
    title: "Publish or automate within your limits",
    detail: "Publish immediately or within per-role approval limits, with a read-back check and a one-click rollback.",
  },
];

export default function Workflow() {
  return (
    <section className="section section-subtle" id="how-it-works">
      <div className="section-width">
        <h2>How it works</h2>
        <p className="section-lede">Four steps from a connected store to a published price you can defend.</p>
        <ol className="workflow">
          {STEPS.map((step, index) => (
            <li key={step.title} className="card workflow-step">
              <span className="workflow-number" aria-hidden="true">
                {index + 1}
              </span>
              <div>
                <h3>{step.title}</h3>
                <p className="muted">{step.detail}</p>
              </div>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}
