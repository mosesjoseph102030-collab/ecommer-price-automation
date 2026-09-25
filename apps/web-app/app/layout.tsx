import "./globals.css";

export const metadata = {
  title: "Store dashboard | Pricing Intelligence",
  description: "WooCommerce catalog and connection management",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" data-theme="teal-ledger">
      <body>{children}</body>
    </html>
  );
}
