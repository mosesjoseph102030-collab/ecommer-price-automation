import WooCommercePanel from "@/components/WooCommercePanel";

export default function WooCommerceSettingsPage({ params }: { params: { slug: string } }) {
  return <>
    <header className="page-header"><div><p className="muted">Settings / Integrations</p><h1>WooCommerce connection</h1><p className="muted">Connect, monitor, and reconcile your catalog. Credentials are write-only.</p></div></header>
    <WooCommercePanel slug={params.slug} />
  </>;
}
