import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = { title: "Pricing Intelligence", description: "Safer WooCommerce pricing for independent stores" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" data-theme="teal-ledger">
      <body>
        <header className="site-header"><strong>FinTrade</strong><nav aria-label="Main navigation"><a className="optional" href="/#features">Features</a><a className="optional" href="/#how-it-works">How it works</a><a href="/signin">Sign in</a><a href="/signup">Start free</a></nav></header>
        {children}
        <footer className="site-footer">Product · Resources · Company · Legal · Status · Contact</footer>
      </body>
    </html>
  );
}
