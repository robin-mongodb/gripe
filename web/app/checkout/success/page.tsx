import Link from "next/link";
import { fmtMoney } from "../../../lib/api";

// Server component: everything it needs arrives via query params from the checkout redirect.
export default function SuccessPage({ searchParams }: { searchParams: Record<string, string | undefined> }) {
  const amount = Number(searchParams.amount ?? 0);
  const currency = searchParams.currency ?? "GBP";
  const status = searchParams.status ?? "captured";
  return (
    <section className="mx-auto max-w-md text-center">
      <div className="rounded-lg border bg-white p-8 shadow-sm">
        <div className="text-4xl">✅</div>
        <h1 className="mt-2 text-2xl font-semibold">Payment {status}</h1>
        <p className="mt-2 text-lg">{fmtMoney(amount, currency)}</p>
        <p className="mt-1 text-xs text-neutral-500">
          {searchParams.method} · payment {searchParams.id}
        </p>
        {status === "pending" || status === "authorized" ? (
          <p className="mt-3 text-sm text-neutral-600">
            This method settles asynchronously — the merchant will see it move to captured/settled later.
          </p>
        ) : null}
        <Link href="/checkout" className="mt-6 inline-block rounded bg-neutral-900 px-4 py-2 text-sm text-white">
          New payment
        </Link>
      </div>
    </section>
  );
}
