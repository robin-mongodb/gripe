import Link from "next/link";

export default function Home() {
  const surfaces = [
    { href: "/admin",    title: "Admin",    body: "Every merchant, every payment. Ops + support." },
    { href: "/merchant", title: "Merchant", body: "Your own payments. Refund, manage subscriptions." },
    { href: "/checkout", title: "Checkout", body: "Customer-facing pay surface." },
  ];
  return (
    <div className="grid gap-4 sm:grid-cols-3">
      {surfaces.map(s => (
        <Link key={s.href} href={s.href} className="rounded-lg border bg-white p-4 shadow-sm hover:shadow">
          <h2 className="text-lg font-semibold">{s.title}</h2>
          <p className="mt-1 text-sm text-neutral-600">{s.body}</p>
        </Link>
      ))}
    </div>
  );
}
