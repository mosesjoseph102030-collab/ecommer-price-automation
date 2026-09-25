import RecommendationManager from "@/components/RecommendationManager";

export default function RecommendationsPage({ params }: { params: { slug: string } }) {
  return (
    <>
      <header className="page-header">
        <div>
          <p className="muted">Pricing</p>
          <h1>Recommendations</h1>
          <p className="muted">Review, approve, publish, and roll back WooCommerce price changes.</p>
        </div>
      </header>
      <RecommendationManager slug={params.slug} />
    </>
  );
}
