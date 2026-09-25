const DASHBOARD_URL = process.env.NEXT_PUBLIC_DASHBOARD_URL ?? "http://localhost:3001";

// Navigation targets are on-page anchors rather than separate routes. The spec
// lists /features, /how-it-works and /pricing as distinct public routes, but the
// sections they describe are the ones the homepage wireframe requires, so linking
// to the anchors keeps every header link working today without a dead page.
// Promoting a section to its own route later is a content decision, not a
// navigation rewrite, because the ids already exist.
const LINKS = [
  { href: "#features", label: "Features" },
  { href: "#how-it-works", label: "How it works" },
  { href: "#pricing", label: "Pricing" },
  // Labelled for the section it actually lands on. The spec lists /community as a
  // public route, but no public community page exists yet, so linking "Community"
  // at a section about use cases would be a mislabelled link rather than a
  // working one.
  { href: "#use-cases", label: "Use cases" },
];

export default function SiteHeader() {
  return (
    <header className="site-header">
      <a className="brand" href="/">
        FinTrade
      </a>
      <nav aria-label="Main">
        {LINKS.map((link) => (
          <a key={link.href} className="optional" href={link.href}>
            {link.label}
          </a>
        ))}
        <a href="/signin">Sign in</a>
        <a className="button button-primary" href="/signup">
          Start free
        </a>
      </nav>
      <a className="dashboard-link optional" href={DASHBOARD_URL}>
        Dashboard
      </a>
    </header>
  );
}
