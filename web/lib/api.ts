// Thin client for the Gripe API. Auth is out of scope — the actor is whatever
// headers we send (X-Actor-Role / X-Actor-Id), matching the backend's actorMiddleware.
const BASE = process.env.NEXT_PUBLIC_API_BASE ?? "/v1";

export type ActorRole = "admin" | "merchant" | "customer";

export interface Payment {
  id: string;
  merchant_id: string;
  customer_id: string;
  amount_minor: number;
  currency: string;
  method: string;
  status: string;
  refunded_minor: number;
  created_at: string;
  updated_at: string;
}

export interface Refund {
  id: string;
  payment_id: string;
  amount_minor: number;
  currency: string;
  created_at: string;
}

export interface Page {
  items: Payment[];
  next_cursor?: string;
}

export interface Balance {
  currency: string;
  balance_minor: number;
  fees_minor: number;
}

export interface MerchantBalanceRow extends Balance {
  merchant_id: string;
}

export interface CurrencyTotal {
  currency: string;
  total_minor: number;
}

export interface MerchantDailyVolume {
  merchant_id: string;
  day: string;
  currency: string;
  total_minor: number;
  count: number;
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

interface ApiOptions {
  role: ActorRole;
  actorId: string;
  method?: "GET" | "POST";
  body?: unknown;
  idempotencyKey?: string;
}

export async function api<T>(path: string, opts: ApiOptions): Promise<T> {
  const headers: Record<string, string> = {
    "X-Actor-Role": opts.role,
    "X-Actor-Id": opts.actorId,
  };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  if (opts.idempotencyKey) headers["Idempotency-Key"] = opts.idempotencyKey;

  const res = await fetch(`${BASE}${path}`, {
    method: opts.method ?? "GET",
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  const data: unknown = await res.json().catch(() => ({}));
  if (!res.ok) {
    const msg =
      typeof data === "object" && data !== null && "error" in data
        ? String((data as { error: unknown }).error)
        : res.statusText;
    throw new ApiError(res.status, msg);
  }
  return data as T;
}

// Display helper: 5013 minor units of GBP -> "£50.13".
const SYMBOLS: Record<string, string> = { USD: "$", GBP: "£", EUR: "€" };
export function fmtMoney(minor: number, currency: string): string {
  const sym = SYMBOLS[currency] ?? `${currency} `;
  return `${sym}${(minor / 100).toFixed(2)}`;
}

// Parse "50.13" -> 5013. Throws on invalid or negative input (money path).
export function toMinor(amount: string): number {
  if (!/^\d+(\.\d{1,2})?$/.test(amount.trim())) {
    throw new Error("amount must look like 50 or 50.13");
  }
  return Math.round(Number(amount.trim()) * 100);
}
