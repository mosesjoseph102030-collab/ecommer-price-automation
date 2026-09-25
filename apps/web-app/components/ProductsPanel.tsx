"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { api, ApiError, type Product } from "@/lib/api";

const money = (kobo?: number) => kobo === undefined ? "—" : new Intl.NumberFormat("en-NG", { style: "currency", currency: "NGN" }).format(kobo / 100);

export default function ProductsPanel({ slug }: { slug: string }) {
  const [products, setProducts] = useState<Product[]>([]);
  const [next, setNext] = useState<string | undefined>();
  const [cursor, setCursor] = useState<string | undefined>();
  const [cursorHistory, setCursorHistory] = useState<Array<string | undefined>>([]);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async (after?: string) => {
    setLoading(true); setError("");
    const params = new URLSearchParams({ limit: "50" });
    if (search) params.set("q", search);
    if (status) params.set("status", status);
    if (after) params.set("after", after);
    try { const page = await api.products(slug, `?${params}`); setProducts(page.products); setNext(page.next_cursor); }
    catch (cause) { setError(cause instanceof ApiError ? cause.message : "Could not load products."); }
    finally { setLoading(false); }
  }, [slug, search, status]);

  useEffect(() => { const timer = setTimeout(() => void load(), 250); return () => clearTimeout(timer); }, [load]);

  return <section className="panel">
    <div className="row between" style={{ marginBottom: 16 }}>
      <div><h2>Catalog</h2><p className="muted">Imported WooCommerce products and variants.</p></div>
      <form className="row" onSubmit={e => { e.preventDefault(); setCursor(undefined); void load(); }}>
        <div className="field"><label className="muted" htmlFor="product-search">Search</label><input id="product-search" value={search} onChange={e => setSearch(e.target.value)} placeholder="Name or SKU" /></div>
        <div className="field"><label className="muted" htmlFor="status-filter">Status</label><select id="status-filter" value={status} onChange={e => { setStatus(e.target.value); setCursor(undefined); }}><option value="">All</option>{["published", "draft", "private", "archived", "deleted"].map(s => <option key={s}>{s}</option>)}</select></div>
        <button className="btn" style={{ alignSelf: "end" }}>Apply</button>
      </form>
    </div>
    {error && <div className="alert alert-error">{error}</div>}
    {loading ? <div className="loading">Loading catalog…</div> : products.length === 0 ? <p className="muted">No products match. Connect a store or start a catalog sync.</p> : (
      <div className="table-wrap"><table><thead><tr><th>Product</th><th>SKU</th><th>Status</th><th>Stock</th><th>Price</th><th>Sale</th><th>Variants</th><th>Review</th><th>Cost</th></tr></thead><tbody>
        {products.map(product => <tr key={product.id}>
          <td><strong>{product.name}</strong>{product.categories.length > 0 && <div className="muted">{product.categories.join(", ")}</div>}</td>
          <td>{product.sku || <span className="badge badge-warning">Missing</span>}</td>
          <td><span className={`badge ${product.status === "published" ? "badge-success" : product.status === "deleted" ? "badge-error" : "badge-warning"}`}>{product.status}</span></td>
          <td>{product.stock_status}{product.stock_quantity !== undefined ? ` (${product.stock_quantity})` : ""}</td>
          <td>{money(product.price_kobo)}</td><td>{money(product.sale_price_kobo)}</td><td>{product.variant_count}</td>
          <td>{product.price_conflict ? <span className="badge badge-error">Price conflict</span> : product.needs_review ? <span className="badge badge-warning">{product.review_reason.replaceAll("_", " ")}</span> : "—"}</td>
          <td><Link className="btn" href={`/app/${slug}/costs?product=${encodeURIComponent(product.id)}`}>Cost</Link></td>
        </tr>)}
      </tbody></table></div>
    )}
    <div className="row between" style={{ marginTop: 16 }}>
      <button className="btn" disabled={cursorHistory.length === 0 || loading} onClick={() => {
        const history = [...cursorHistory]; const previous = history.pop(); setCursorHistory(history); setCursor(previous); void load(previous);
      }}>Previous</button>
      <button className="btn" disabled={!next || loading} onClick={() => { setCursorHistory(history => [...history, cursor]); setCursor(next); void load(next); }}>Next</button>
    </div>
  </section>;
}
