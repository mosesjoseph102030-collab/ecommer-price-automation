import ProductsPanel from "@/components/ProductsPanel";

export default function ProductsPage({ params }: { params: { slug: string } }) {
  return <>
    <header className="page-header"><div><p className="muted">Catalog</p><h1>Products</h1><p className="muted">WooCommerce products, variants, stock, and review status.</p></div></header>
    <ProductsPanel slug={params.slug} />
  </>;
}
