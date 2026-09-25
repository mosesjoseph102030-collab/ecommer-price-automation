// The closing line is the wireframe's, verbatim.
export default function FinalCta() {
  return (
    <section className="final-cta">
      <div className="section-width">
        <h2>Connect your WooCommerce store and see your first recommendations.</h2>
        <p className="muted">
          Start with a free trial. No card required, and your existing prices are not touched until you approve a
          change.
        </p>
        <div className="hero-actions">
          <a className="button button-primary" href="/signup">
            Start free
          </a>
          <a className="button button-ghost" href="/signin">
            Sign in
          </a>
        </div>
      </div>
    </section>
  );
}
