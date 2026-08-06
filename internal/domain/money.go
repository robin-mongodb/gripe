package domain

// Gripe's cut, in basis points (task 49). 300bp = 3%.
const GripeFeeBasisPoints int64 = 300

// GripeFee returns the platform fee on an amount, rounded half-even to the
// smallest currency unit. All supported currencies (USD/GBP/EUR) have 2dp
// minor units, so no per-currency scaling is needed.
// ponytail: add a currency param when a 0dp currency (JPY) joins the enum.
func GripeFee(amountMinor int64) int64 {
	return divHalfEven(amountMinor*GripeFeeBasisPoints, 10_000)
}

// divHalfEven divides n by d rounding half-to-even (banker's rounding).
// Money path: callers guarantee n >= 0 and d > 0.
func divHalfEven(n, d int64) int64 {
	q, r := n/d, n%d
	switch {
	case 2*r < d:
		return q
	case 2*r > d:
		return q + 1
	case q%2 == 0: // exactly half: round to even
		return q
	default:
		return q + 1
	}
}
