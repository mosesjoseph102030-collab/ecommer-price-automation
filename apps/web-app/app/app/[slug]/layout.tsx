export default function StoreLayout({ children, params }: { children: React.ReactNode; params: { slug: string } }) {
  const base = `/app/${params.slug}`;
  return (
    <div className="app-shell">
      <nav className="app-nav" aria-label="Store navigation">
        <strong>FinTrade</strong>
        <a href={`${base}/overview`}>Overview</a>
        <a href={`${base}/products`}>Products</a>
        <a href={`${base}/costs`}>Costs</a>
        <a href={`${base}/rules`}>Rules</a>
        <a href={`${base}/competitors`}>Competitors</a>
        <a href={`${base}/recommendations`}>Recommendations</a>
        <a href={`${base}/reports`}>Reports</a>
        <a href={`${base}/notifications`}>Alerts</a>
        <a href={`${base}/feedback`}>Feedback</a>
        <a href={`${base}/community`}>Community</a>
        <a href={`${base}/billing`}>Billing</a>
        <a href={`${base}/settings/woocommerce`}>WooCommerce</a>
      </nav>
      <main className="app-main">{children}</main>
    </div>
  );
}
