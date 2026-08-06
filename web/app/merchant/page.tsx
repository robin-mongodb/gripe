"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { api, fmtMoney, type Balance, type Page } from "../../lib/api";

// The "logged in" merchant is just a header value (auth is out of scope).
// Persist it in the URL-free simplest place: component state with a default from the seed.
export default function MerchantPage() {
  const [merchantId, setMerchantId] = useState("mer_seed_000");
  const [page, setPage] = useState<Page | null>(null);
  const [balances, setBalances] = useState<Balance[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(
    async (cursor?: string) => {
      setLoading(true);
      setError(null);
      try {
        const q = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
        const p = await api<Page>(`/payments${q}`, { role: "merchant", actorId: merchantId });
        setPage((prev) =>
          cursor && prev ? { items: [...prev.items, ...p.items], next_cursor: p.next_cursor } : p,
        );
        if (!cursor) {
          const b = await api<{ items: Balance[] }>("/balances", { role: "merchant", actorId: merchantId });
          setBalances(b.items);
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    },
    [merchantId],
  );

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <section>
      <div className="flex items-end justify-between">
        <h1 className="text-2xl font-semibold">Merchant dashboard</h1>
        <label className="text-sm">
          Acting as
          <input
            className="ml-2 rounded border px-2 py-1 text-sm"
            value={merchantId}
            onChange={(e) => setMerchantId(e.target.value)}
          />
        </label>
      </div>
      {error && <p className="mt-4 text-sm text-red-600">{error}</p>}
      {/* One balance row per currency (tasks 56/67). Settled funds net of Gripe's 3% fee. */}
      <div className="mt-4 grid gap-4 sm:grid-cols-3">
        {balances.map((b) => (
          <div key={b.currency} className="rounded-lg border bg-white p-4 shadow-sm">
            <div className="text-xs uppercase text-neutral-500">{b.currency} balance</div>
            <div className="mt-1 text-2xl font-semibold">{fmtMoney(b.balance_minor, b.currency)}</div>
            <div className="mt-1 text-xs text-neutral-500">
              fees paid to Gripe: {fmtMoney(b.fees_minor, b.currency)}
            </div>
          </div>
        ))}
        {balances.length === 0 && (
          <div className="rounded-lg border bg-white p-4 text-sm text-neutral-500 shadow-sm sm:col-span-3">
            No settled funds yet — balances appear once payments settle.
          </div>
        )}
      </div>
      <div className="mt-4 overflow-x-auto rounded-lg border bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead className="border-b bg-neutral-50 text-left text-neutral-600">
            <tr>
              <th className="px-3 py-2">Created</th>
              <th className="px-3 py-2">Amount</th>
              <th className="px-3 py-2">Method</th>
              <th className="px-3 py-2">Status</th>
              <th className="px-3 py-2">Refunded</th>
              <th className="px-3 py-2">Customer</th>
            </tr>
          </thead>
          <tbody>
            {page?.items.map((p) => (
              <tr key={p.id} className="border-b last:border-0 hover:bg-neutral-50">
                <td className="px-3 py-2 whitespace-nowrap">
                  <Link href={`/merchant/payments/${p.id}?as=${merchantId}`} className="text-blue-700 hover:underline">
                    {new Date(p.created_at).toLocaleString()}
                  </Link>
                </td>
                <td className="px-3 py-2">{fmtMoney(p.amount_minor, p.currency)}</td>
                <td className="px-3 py-2">{p.method}</td>
                <td className="px-3 py-2">{p.status}</td>
                <td className="px-3 py-2">{p.refunded_minor > 0 ? fmtMoney(p.refunded_minor, p.currency) : "—"}</td>
                <td className="px-3 py-2 text-neutral-500">{p.customer_id}</td>
              </tr>
            ))}
            {page && page.items.length === 0 && (
              <tr>
                <td colSpan={6} className="px-3 py-6 text-center text-neutral-500">
                  No payments yet for {merchantId}.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {page?.next_cursor && (
        <button
          onClick={() => void load(page.next_cursor)}
          disabled={loading}
          className="mt-4 rounded border bg-white px-4 py-2 text-sm shadow-sm disabled:opacity-50"
        >
          {loading ? "Loading…" : "Load more"}
        </button>
      )}
    </section>
  );
}
