import CostManager from "@/components/CostManager";

export default function CostsPage({ params }: { params: { slug: string } }) {
  return <><header className="page-header"><div><p className="muted">Pricing inputs</p><h1>Costs and margins</h1><p className="muted">Manage landed cost and review minimum profitable prices.</p></div></header><CostManager slug={params.slug} /></>;
}
