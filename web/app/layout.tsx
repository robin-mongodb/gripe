import "./globals.css";
import Link from "next/link";
import type { ReactNode } from "react";

export const metadata = { title: "Gripe", description: "Payments platform" };

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body className="min-h-full bg-neutral-50 text-neutral-900 antialiased">
        <header className="border-b bg-white">
          <nav className="mx-auto flex max-w-6xl items-center gap-6 px-4 py-3 text-sm">
            <Link href="/" className="font-semibold">Gripe</Link>
            <Link href="/admin" className="text-neutral-600 hover:text-neutral-900">Admin</Link>
            <Link href="/merchant" className="text-neutral-600 hover:text-neutral-900">Merchant</Link>
            <Link href="/checkout" className="text-neutral-600 hover:text-neutral-900">Checkout</Link>
          </nav>
        </header>
        <main className="mx-auto max-w-6xl px-4 py-6">{children}</main>
      </body>
    </html>
  );
}
