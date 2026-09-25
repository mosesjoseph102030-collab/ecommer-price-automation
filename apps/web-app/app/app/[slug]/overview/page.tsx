import OverviewPanel from "@/components/OverviewPanel";

export default function Overview({ params }: { params: { slug: string } }) {
  return <><header className="page-header"><div><p className="muted">Store overview</p><h1>Welcome back</h1></div></header><OverviewPanel slug={params.slug} /></>;
}
