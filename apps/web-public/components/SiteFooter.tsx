// The spec groups the footer into Product / Resources / Company / Legal / Status /
// Contact. Only the entries that resolve are rendered: a footer link to a page
// that does not exist is worse than a missing link, because it sends a prospect
// to a dead end and reads as a broken product.
const COLUMNS = [
  {
    title: "Product",
    links: [
      { href: "#features", label: "Features" },
      { href: "#how-it-works", label: "How it works" },
      { href: "#pricing", label: "Pricing" },
    ],
  },
  {
    title: "Resources",
    links: [
      { href: "#how-it-works", label: "Getting started" },
      { href: "#faq", label: "FAQ" },
    ],
  },
  {
    title: "Company",
    links: [{ href: "#use-cases", label: "Use cases" }],
  },
];

// Security, Legal, and Status are deliberately absent rather than pointing at
// /security and /terms. Those routes are in the spec but not built, and
// publishing a legal page that says nothing is a compliance problem, not a
// navigation win. They belong with the public sub-pages, not with a placeholder.
const STATUS_URL = process.env.NEXT_PUBLIC_STATUS_URL ?? "http://localhost:8080/api/v1/status";

export default function SiteFooter() {
  return (
    <footer className="site-footer">
      <div className="footer-grid">
        <div className="footer-brand">
          <strong>FinTrade</strong>
          <p className="muted">
            Pricing intelligence for independent WooCommerce stores. Protect your margin, watch competitors, and
            publish prices with an audit trail.
          </p>
        </div>
        {COLUMNS.map((column) => (
          <div key={column.title}>
            <h3>{column.title}</h3>
            <ul>
              {column.links.map((link) => (
                <li key={`${column.title}-${link.label}`}>
                  <a href={link.href}>{link.label}</a>
                </li>
              ))}
            </ul>
          </div>
        ))}
        <div>
          <h3>Status</h3>
          <ul>
            <li>
              {/* The status feed is served by the API and needs no session, so it
                  works from a customer's own status page. */}
              <a href={STATUS_URL} rel="noreferrer noopener" target="_blank">
                System status
              </a>
            </li>
          </ul>
        </div>
      </div>
      <div className="footer-base">
        <p className="muted">
          Payments are processed by Paystack. FinTrade never stores card details. Prices are shown in Nigerian Naira.
        </p>
      </div>
    </footer>
  );
}
