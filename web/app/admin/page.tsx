"use client";

import { useCallback, useEffect, useState } from "react";
import {
  api,
  fmtMoney,
  type CurrencyTotal,
  type MerchantBalanceRow,
  type MerchantDailyVolume,
} from "../../lib/api";

// Employee console: volume (task 25), per-merchant balances + Gripe revenue (tasks 57/68).
export default function AdminPage() {
  const [rows, setRows] = useState<MerchantDailyVolume[] | null>(null);
  const [balances, setBalances] = useState<MerchantBalanceRow[]>([]);
  const [revenue, setRevenue] = useState<CurrencyTotal[]>([]);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setError(null);
    const admin = { role: "admin" as const, actorId: "admin" };
    try {
      const [vol, bal, rev] = await Promise.all([
        api<{ items: MerchantDailyVolume[] }>("/reports/volume", admin),
        api<{ items: MerchantBalanceRow[] }>("/reports/balances", admin),
        api<{ items: CurrencyTotal[] }>("/reports/revenue", admin),
      ]);
      setRows(vol.items);
      setBalances(bal.items);
      setRevenue(rev.items);
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
      {error && <p className="mt-4 text-sm text-red-600">{error}</p>}

      {/* Gripe revenue split by currency (task 68). */}
      <h2 className="mt-4 text-sm font-semibold text-neutral-700">Gripe revenue</h2>
      <div className="mt-2 grid gap-4 sm:grid-cols-3">
        {revenue.map((r) => (
          <div key={r.currency} className="rounded-lg border bg-white p-4 shadow-sm">
            <div className="text-xs uppercase text-neutral-500">{r.currency}</div>
            <div className="mt-1 text-2xl font-semibold">{fmtMoney(r.total_minor, r.currency)}</div>
          </div>
        ))}
        {revenue.length === 0 && (
          <div className="rounded-lg border bg-white p-4 text-sm text-neutral-500 shadow-sm sm:col-span-3">
            No fee revenue yet — revenue accrues as payments settle.
          </div>
        )}
      </div>

      {/* Per-merchant balances (task 57). */}
      <h2 className="mt-6 text-sm font-semibold text-neutral-700">Merchant balances</h2>
      <div className="mt-2 overflow-x-auto rounded-lg border bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead className="border-b bg-neutral-50 text-left text-neutral-600">
            <tr>
              <th className="px-3 py-2">Merchant</th>
              <th className="px-3 py-2">Currency</th>
              <th className="px-3 py-2 text-right">Balance</th>
              <th className="px-3 py-2 text-right">Fees paid</th>
            </tr>
          </thead>
          <tbody>
            {balances.map((b) => (
              <tr key={`${b.merchant_id}/${b.currency}`} className="border-b last:border-0">
                <td className="px-3 py-2 font-mono">{b.merchant_id}</td>
                <td className="px-3 py-2">{b.currency}</td>
                <td className="px-3 py-2 text-right">{fmtMoney(b.balance_minor, b.currency)}</td>
                <td className="px-3 py-2 text-right">{fmtMoney(b.fees_minor, b.currency)}</td>
              </tr>
            ))}
            {balances.length === 0 && (
              <tr>
                <td colSpan={4} className="px-3 py-6 text-center text-neutral-500">
                  No balances yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <h2 className="mt-6 text-sm font-semibold text-neutral-700">Volume per merchant per day (last 30 days, declined excluded)</h2>
      <div className="mt-2 overflow-x-auto rounded-lg border bg-white shadow-sm">
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
