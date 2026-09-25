import CompetitorManager from "@/components/CompetitorManager";

export default function CompetitorsPage({params}:{params:{slug:string}}){return <><header className="page-header"><div><p className="muted">Market intelligence</p><h1>Competitors</h1><p className="muted">Approved-source observations and human-reviewed product matching.</p></div></header><CompetitorManager slug={params.slug}/></>}
