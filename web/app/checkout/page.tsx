"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { api, newIdempotencyKey, toMinor, type Payment } from "../../lib/api";

const METHODS = [
  { value: "card", label: "Card" },
  { value: "apple_pay", label: "Apple Pay" },
  { value: "google_pay", label: "Google Pay" },
  { value: "direct_debit", label: "Direct Debit" },
  { value: "bank_transfer", label: "Bank Transfer" },
];

export default function CheckoutPage() {
  const router = useRouter();
  const [merchantId, setMerchantId] = useState("mer_seed_000");
  const [customerId, setCustomerId] = useState("cus_demo");
  const [amount, setAmount] = useState("50.00");
  const [currency, setCurrency] = useState("GBP");
  const [method, setMethod] = useState("card");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function pay(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      const p = await api<Payment>("/payments", {
        role: "customer",
        actorId: customerId,
        method: "POST",
        idempotencyKey: newIdempotencyKey(),
        body: {
          merchant_id: merchantId,
          customer_id: customerId,
          amount_minor: toMinor(amount),
          currency,
          method,
        },
      });
      const q = new URLSearchParams({
        id: p.id,
        status: p.status,
        amount: String(p.amount_minor),
        currency: p.currency,
        method: p.method,
      });
      router.push(p.status === "declined" ? `/checkout/failure?${q}` : `/checkout/success?${q}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setSubmitting(false);
    }
  }

  const field = "mt-1 w-full rounded border px-3 py-2 text-sm";
  return (
    <section className="mx-auto max-w-md">
      <h1 className="text-2xl font-semibold">Checkout</h1>
      <p className="mt-1 text-sm text-neutral-600">
        Mock payment surface. Amounts ending in <code>.13</code> are declined.
      </p>
      <form onSubmit={pay} className="mt-4 space-y-4 rounded-lg border bg-white p-4 shadow-sm">
        <label className="block text-sm">
          Merchant
          <input className={field} value={merchantId} onChange={(e) => setMerchantId(e.target.value)} required />
        </label>
        <label className="block text-sm">
          Customer
          <input className={field} value={customerId} onChange={(e) => setCustomerId(e.target.value)} required />
        </label>
        <div className="flex gap-3">
          <label className="block flex-1 text-sm">
            Amount
            <input className={field} value={amount} onChange={(e) => setAmount(e.target.value)} inputMode="decimal" required />
          </label>
          <label className="block text-sm">
            Currency
            <select className={field} value={currency} onChange={(e) => setCurrency(e.target.value)}>
              <option>GBP</option>
              <option>USD</option>
              <option>EUR</option>
            </select>
          </label>
        </div>
        <fieldset className="text-sm">
          <legend>Payment method</legend>
          <div className="mt-1 grid grid-cols-2 gap-2">
            {METHODS.map((m) => (
              <label
                key={m.value}
                className={`cursor-pointer rounded border px-3 py-2 ${method === m.value ? "border-neutral-900 bg-neutral-100" : ""}`}
              >
                <input
                  type="radio"
                  name="method"
                  value={m.value}
                  checked={method === m.value}
                  onChange={() => setMethod(m.value)}
                  className="mr-2"
                />
                {m.label}
              </label>
            ))}
          </div>
        </fieldset>
        {error && <p className="text-sm text-red-600">{error}</p>}
        <button
          type="submit"
          disabled={submitting}
          className="w-full rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50"
        >
          {submitting ? "Paying…" : "Pay"}
        </button>
      </form>
    </section>
  );
}
