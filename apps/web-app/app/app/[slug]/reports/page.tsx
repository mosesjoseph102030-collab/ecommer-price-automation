import ReportManager from "@/components/ReportManager";

export default function ReportsPage({ params }: { params: { slug: string } }) {
  return (
    <>
      <header className="page-header">
        <div>
          <p className="muted">Insights</p>
          <h1>Reports</h1>
          <p className="muted">Margins, competitor gaps, price-change history, and publish reliability.</p>
        </div>
      </header>
      <ReportManager slug={params.slug} />
    </>
  );
}
