// Eyebrow, headline, and description are the exact strings in the file.md
// homepage wireframe. Changing them is a copy decision, so they are kept verbatim
// rather than paraphrased.
export default function Hero() {
  return (
    <section className="hero">
      <div className="hero-inner">
        <p className="hero-eyebrow">WooCommerce price intelligence for small stores</p>
        <h1>Price confidently. Protect every margin.</h1>
        <p className="hero-description">
          Monitor competitors, calculate your safe price floor, and approve WooCommerce price updates from one calm
          dashboard.
        </p>
        <div className="hero-actions">
          <a className="button button-primary" href="/signup">
            Start free
          </a>
          <a className="button button-ghost" href="#how-it-works">
            Watch how it works
          </a>
        </div>
      </div>
    </section>
  );
}
