import FeedbackManager from "@/components/FeedbackManager";

export default function FeedbackPage({ params }: { params: { slug: string } }) {
  return (
    <>
      <header className="page-header">
        <div>
          <p className="muted">Support</p>
          <h1>Feedback</h1>
          <p className="muted">Private thread with the product and support team.</p>
        </div>
      </header>
      <FeedbackManager slug={params.slug} />
    </>
  );
}
