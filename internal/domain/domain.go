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
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type CreatePaymentInput struct {
	MerchantID  string        `json:"merchant_id"`
	CustomerID  string        `json:"customer_id"`
	AmountMinor int64         `json:"amount_minor"`
	Currency    Currency      `json:"currency"`
	Method      PaymentMethod `json:"method"`
}

type Refund struct {
	ID          string    `json:"id"`
	PaymentID   string    `json:"payment_id"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    Currency  `json:"currency"`
	CreatedAt   time.Time `json:"created_at"`
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

// Balance is a per-currency ledger row for a merchant.
type Balance struct {
	Currency     Currency `json:"currency"`
	BalanceMinor int64    `json:"balance_minor"`
}
