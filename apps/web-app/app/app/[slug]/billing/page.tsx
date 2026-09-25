import BillingManager from "@/components/BillingManager";

export default function BillingPage({ params }: { params: { slug: string } }) {
  return (
    <>
      <header className="page-header">
        <div>
          <p className="muted">Account</p>
          <h1>Billing</h1>
          <p className="muted">
            Plan, usage, and payment history. Payment runs through Paystack; this store never sees card details.
          </p>
        </div>
      </header>
      <BillingManager slug={params.slug} />
    </>
  );
}
