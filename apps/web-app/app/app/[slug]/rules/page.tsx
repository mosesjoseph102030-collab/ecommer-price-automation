import RulesManager from "@/components/RulesManager";

export default function RulesPage({ params }: { params: { slug: string } }) {
  return <><header className="page-header"><div><p className="muted">Pricing controls</p><h1>Pricing rules</h1><p className="muted">Configure deterministic margin floors and preview rule impact.</p></div></header><RulesManager slug={params.slug} /></>;
}
