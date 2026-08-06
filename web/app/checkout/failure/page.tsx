import Link from "next/link";
import { fmtMoney } from "../../../lib/api";

export default function FailurePage({ searchParams }: { searchParams: Record<string, string | undefined> }) {
  const amount = Number(searchParams.amount ?? 0);
  const currency = searchParams.currency ?? "GBP";
  return (
    <section className="mx-auto max-w-md text-center">
      <div className="rounded-lg border border-red-200 bg-white p-8 shadow-sm">
        <div className="text-4xl">❌</div>
        <h1 className="mt-2 text-2xl font-semibold">Payment declined</h1>
        <p className="mt-2 text-lg">{fmtMoney(amount, currency)}</p>
        <p className="mt-1 text-xs text-neutral-500">
          {searchParams.method} · payment {searchParams.id}
        </p>
        <p className="mt-3 text-sm text-neutral-600">The payment was declined by the (mock) issuer. Try a different amount.</p>
        <Link href="/checkout" className="mt-6 inline-block rounded bg-neutral-900 px-4 py-2 text-sm text-white">
          Try again
        </Link>
      </div>
    </section>
  );
}
