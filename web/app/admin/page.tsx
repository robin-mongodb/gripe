"use client";

import { useCallback, useEffect, useState } from "react";
import { api, fmtMoney, type MerchantDailyVolume } from "../../lib/api";

// Employee console (task 25): cross-merchant volume per day per currency.
// Defaults to the API's default window (last 30 days).
export default function AdminPage() {
  const [rows, setRows] = useState<MerchantDailyVolume[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      const res = await api<{ items: MerchantDailyVolume[] }>("/reports/volume", {
        role: "admin",
        actorId: "admin",
      });
      setRows(res.items);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <section>
      <h1 className="text-2xl font-semibold">Admin console</h1>
      <p className="mt-1 text-sm text-neutral-600">Volume per merchant per day, last 30 days. Declined payments excluded.</p>
      {error && <p className="mt-4 text-sm text-red-600">{error}</p>}
      <div className="mt-4 overflow-x-auto rounded-lg border bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead className="border-b bg-neutral-50 text-left text-neutral-600">
            <tr>
              <th className="px-3 py-2">Merchant</th>
              <th className="px-3 py-2">Day</th>
              <th className="px-3 py-2">Currency</th>
              <th className="px-3 py-2 text-right">Payments</th>
              <th className="px-3 py-2 text-right">Volume</th>
            </tr>
          </thead>
          <tbody>
            {rows?.map((r) => (
              <tr key={`${r.merchant_id}/${r.day}/${r.currency}`} className="border-b last:border-0">
                <td className="px-3 py-2 font-mono">{r.merchant_id}</td>
                <td className="px-3 py-2">{r.day}</td>
                <td className="px-3 py-2">{r.currency}</td>
                <td className="px-3 py-2 text-right">{r.count}</td>
                <td className="px-3 py-2 text-right">{fmtMoney(r.total_minor, r.currency)}</td>
              </tr>
            ))}
            {rows && rows.length === 0 && (
              <tr>
                <td colSpan={5} className="px-3 py-6 text-center text-neutral-500">
                  No volume in the last 30 days.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}
