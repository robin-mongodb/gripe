"use client";

import Link from "next/link";
import { useParams, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { api, fmtMoney, toMinor, type Payment, type Refund } from "../../../../lib/api";

// Payment detail + refund action (task 23). Refundable states mirror the store's
// rules: captured/settled/refunded (partial) can be refunded up to the remainder.
export default function PaymentDetailPage() {
  const { id } = useParams<{ id: string }>();
  const merchantId = useSearchParams().get("as") ?? "mer_seed_000";

  const [payment, setPayment] = useState<Payment | null>(null);
  const [amount, setAmount] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<Refund | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const p = await api<Payment>(`/payments/${id}`, { role: "merchant", actorId: merchantId });
      setPayment(p);
      setAmount(((p.amount_minor - p.refunded_minor) / 100).toFixed(2));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [id, merchantId]);

  useEffect(() => {
    void load();
  }, [load]);

  async function refund(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const r = await api<Refund>(`/payments/${id}/refunds`, {
        role: "merchant",
        actorId: merchantId,
        method: "POST",
        idempotencyKey: crypto.randomUUID(),
        body: { amount_minor: toMinor(amount) },
      });
      setDone(r);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  if (!payment) {
    return <p className="text-sm text-neutral-600">{error ?? "Loading…"}</p>;
  }

  const remaining = payment.amount_minor - payment.refunded_minor;
  const refundable = ["captured", "settled", "refunded"].includes(payment.status) && remaining > 0;

  const rows: Array<[string, string]> = [
    ["ID", payment.id],
    ["Status", payment.status],
    ["Amount", fmtMoney(payment.amount_minor, payment.currency)],
    ["Refunded", fmtMoney(payment.refunded_minor, payment.currency)],
    ["Method", payment.method],
    ["Customer", payment.customer_id],
    ["Created", new Date(payment.created_at).toLocaleString()],
  ];

  return (
    <section className="mx-auto max-w-lg">
      <Link href="/merchant" className="text-sm text-blue-700 hover:underline">
        ← Back to payments
      </Link>
      <h1 className="mt-2 text-2xl font-semibold">Payment</h1>
      <dl className="mt-4 rounded-lg border bg-white p-4 text-sm shadow-sm">
        {rows.map(([k, v]) => (
          <div key={k} className="flex justify-between border-b py-1.5 last:border-0">
            <dt className="text-neutral-500">{k}</dt>
            <dd className="font-mono">{v}</dd>
          </div>
        ))}
      </dl>

      {refundable ? (
        <form onSubmit={refund} className="mt-4 rounded-lg border bg-white p-4 shadow-sm">
          <h2 className="text-sm font-semibold">Refund</h2>
          <p className="mt-1 text-xs text-neutral-500">
            Up to {fmtMoney(remaining, payment.currency)} remaining.
          </p>
          <div className="mt-2 flex gap-2">
            <input
              className="flex-1 rounded border px-3 py-2 text-sm"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              inputMode="decimal"
              required
            />
            <button
              type="submit"
              disabled={busy}
              className="rounded bg-red-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50"
            >
              {busy ? "Refunding…" : "Refund"}
            </button>
          </div>
        </form>
      ) : (
        <p className="mt-4 text-sm text-neutral-500">
          Not refundable ({payment.status === "declined" ? "payment was declined" : remaining === 0 ? "fully refunded" : `status is ${payment.status}`}).
        </p>
      )}
      {done && (
        <p className="mt-3 text-sm text-green-700">
          Refund {done.id} issued for {fmtMoney(done.amount_minor, done.currency)}.
        </p>
      )}
      {error && <p className="mt-3 text-sm text-red-600">{error}</p>}
    </section>
  );
}
