import "./globals.css";

export const metadata = { title: "Platform administration" };
export default function AdminLayout({ children }: { children: React.ReactNode }) {
  return <html lang="en" data-theme="teal-ledger"><body><header className="admin-header"><strong>Platform Admin</strong><a href="/admin/integrations">WooCommerce</a><a href="/admin/competitors">Competitor sources</a><a href="/admin/pricing-kill-switch">Kill switch</a><a href="/admin/announcements">Announcements</a><a href="/admin/community">Moderation</a><a href="/admin/audit-logs">Audit logs</a></header><main className="admin-main">{children}</main></body></html>;
}
