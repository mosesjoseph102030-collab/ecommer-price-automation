import CommunityManager from "@/components/CommunityManager";

export default function CommunityPage({ params }: { params: { slug: string } }) {
  return (
    <>
      <header className="page-header">
        <div>
          <p className="muted">Peers</p>
          <h1>Community</h1>
          <p className="muted">Verified store owners. General strategy only — no coordinated pricing.</p>
        </div>
      </header>
      <CommunityManager slug={params.slug} />
    </>
  );
}
