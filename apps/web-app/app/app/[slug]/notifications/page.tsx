import NotificationManager from "@/components/NotificationManager";

export default function NotificationsPage({ params }: { params: { slug: string } }) {
  return (
    <>
      <header className="page-header">
        <div>
          <p className="muted">Alerts</p>
          <h1>Notifications</h1>
          <p className="muted">Publish results, failures, approvals, and incident notices.</p>
        </div>
      </header>
      <NotificationManager slug={params.slug} />
    </>
  );
}
