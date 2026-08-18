package domain

import (
	"time"
)

type Currency string

const (
	USD Currency = "USD"
	GBP Currency = "GBP"
	EUR Currency = "EUR"
)

func (c Currency) Valid() bool { return c == USD || c == GBP || c == EUR }

type PaymentMethod string

const (
	MethodCard         PaymentMethod = "card"
	MethodDirectDebit  PaymentMethod = "direct_debit"
	MethodBankTransfer PaymentMethod = "bank_transfer"
	MethodApplePay     PaymentMethod = "apple_pay"
	MethodGooglePay    PaymentMethod = "google_pay"
)

func (m PaymentMethod) Valid() bool {
	switch m {
	case MethodCard, MethodDirectDebit, MethodBankTransfer, MethodApplePay, MethodGooglePay:
		return true
	}
	return false
}

type PaymentStatus string

const (
	StatusAuthorized PaymentStatus = "authorized"
	StatusCaptured   PaymentStatus = "captured"
	StatusSettled    PaymentStatus = "settled"
	StatusPending    PaymentStatus = "pending"
	StatusDeclined   PaymentStatus = "declined"
	StatusRefunded   PaymentStatus = "refunded"
)

// Payment. AmountMinor is the smallest currency unit as an integer (2dp → cents/pence).
// ponytail: keep money as int64 minor units end-to-end; convert to decimal only at the DB boundary.
type Payment struct {
	ID            string        `json:"id"`
	MerchantID    string        `json:"merchant_id"`
	CustomerID    string        `json:"customer_id"`
	AmountMinor   int64         `json:"amount_minor"`
	Currency      Currency      `json:"currency"`
	Method        PaymentMethod `json:"method"`
	Status        PaymentStatus `json:"status"`
	RefundedMinor int64         `json:"refunded_minor"`
	// NetworkFeeMinor is the mock card-network fee set once by the fee-worker.
	// Bookkeeping only — it never moves merchant balances. 0 = not yet set.
	NetworkFeeMinor int64     `json:"network_fee_minor,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreatePaymentInput struct {
	MerchantID  string        `json:"merchant_id"`
	CustomerID  string        `json:"customer_id"`
	AmountMinor int64         `json:"amount_minor"`
	Currency    Currency      `json:"currency"`
	Method      PaymentMethod `json:"method"`
}

type RefundStatus string

const (
	RefundCreated RefundStatus = "created"
	RefundSettled RefundStatus = "settled" // balance debit applied
)

type Refund struct {
	ID          string       `json:"id"`
	PaymentID   string       `json:"payment_id"`
	MerchantID  string       `json:"merchant_id"`
	AmountMinor int64        `json:"amount_minor"`
	Currency    Currency     `json:"currency"`
	Status      RefundStatus `json:"status"`
	CreatedAt   time.Time    `json:"created_at"`
}

type SubscriptionCadence string

const (
	CadenceDaily   SubscriptionCadence = "daily"
	CadenceWeekly  SubscriptionCadence = "weekly"
	CadenceMonthly SubscriptionCadence = "monthly"
)

func (c SubscriptionCadence) Valid() bool {
	return c == CadenceDaily || c == CadenceWeekly || c == CadenceMonthly
}

// Advance returns t plus one cadence unit. Monthly adds a calendar month (30d approximation
// would drift; use time.AddDate so Jan 31 → Feb 28/29).
// ponytail: no timezone handling — UTC everywhere in this codebase.
func (c SubscriptionCadence) Advance(t time.Time) time.Time {
	switch c {
	case CadenceDaily:
		return t.AddDate(0, 0, 1)
	case CadenceWeekly:
		return t.AddDate(0, 0, 7)
	case CadenceMonthly:
		return t.AddDate(0, 1, 0)
	}
	return t
}

type SubscriptionStatus string

const (
	SubActive    SubscriptionStatus = "active"
	SubCancelled SubscriptionStatus = "cancelled"
)

type Subscription struct {
	ID            string              `json:"id"`
	MerchantID    string              `json:"merchant_id"`
	CustomerID    string              `json:"customer_id"`
	AmountMinor   int64               `json:"amount_minor"`
	Currency      Currency            `json:"currency"`
	Method        PaymentMethod       `json:"method"`
	Cadence       SubscriptionCadence `json:"cadence"`
	Status        SubscriptionStatus  `json:"status"`
	NextChargeAt  time.Time           `json:"next_charge_at"`
	NextCycleIdx  int64               `json:"next_cycle_index"`
	CreatedAt     time.Time           `json:"created_at"`
}

type CreateSubscriptionInput struct {
	MerchantID  string              `json:"merchant_id"`
	CustomerID  string              `json:"customer_id"`
	AmountMinor int64               `json:"amount_minor"`
	Currency    Currency            `json:"currency"`
	Method      PaymentMethod       `json:"method"`
	Cadence     SubscriptionCadence `json:"cadence"`
	StartAt     time.Time           `json:"start_at"`
}

// Actor identifies the caller of a Store read. Actor scoping is part of the use case, not a wrapper concern.
type ActorRole string

const (
	RoleAdmin    ActorRole = "admin"
	RoleMerchant ActorRole = "merchant"
	RoleCustomer ActorRole = "customer"
)

type Actor struct {
	Role ActorRole
	ID   string
}

type Filters struct {
	Status     PaymentStatus
	Method     PaymentMethod
	Currency   Currency
	MerchantID string // admin-only; ignored when actor.Role == RoleMerchant
	FromTime   time.Time
	ToTime     time.Time
	MinMinor   int64
	MaxMinor   int64
}

// Cursor is an opaque pagination token produced by the store.
type Cursor string

type Page struct {
	Items      []Payment `json:"items"`
	NextCursor Cursor    `json:"next_cursor,omitempty"`
}

// Balance is a per-currency ledger row for a merchant. BalanceMinor is what the
// merchant is owed (settled amounts net of fees and settled refunds); FeesMinor is
// the lifetime fee total the merchant has paid to Gripe in that currency.
type Balance struct {
	Currency     Currency `json:"currency"`
	BalanceMinor int64    `json:"balance_minor"`
	FeesMinor    int64    `json:"fees_minor"`
}

// MerchantBalanceRow is one row of the admin balance report: Balance plus who owns it.
type MerchantBalanceRow struct {
	MerchantID   string   `json:"merchant_id"`
	Currency     Currency `json:"currency"`
	BalanceMinor int64    `json:"balance_minor"`
	FeesMinor    int64    `json:"fees_minor"`
}

// CurrencyTotal is one row of the Gripe revenue report.
type CurrencyTotal struct {
	Currency   Currency `json:"currency"`
	TotalMinor int64    `json:"total_minor"`
}

// MerchantDailyVolume is one row of the admin volume report: non-declined payment
// volume for one merchant on one UTC day in one currency. Day is "2006-01-02".
type MerchantDailyVolume struct {
	MerchantID string   `json:"merchant_id"`
	Day        string   `json:"day"`
	Currency   Currency `json:"currency"`
	TotalMinor int64    `json:"total_minor"`
	Count      int64    `json:"count"`
}
