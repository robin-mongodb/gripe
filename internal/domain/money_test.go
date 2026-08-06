package domain

import "testing"

func TestGripeFee(t *testing.T) {
	cases := []struct {
		amount, want int64
	}{
		{10_000, 300}, // £100.00 -> £3.00
		{1_000, 30},
		{100, 3},
		{33, 1},  // 0.99 -> 1 (round up)
		{16, 0},  // 0.48 -> 0
		{50, 2},  // 1.5 -> 2 (even)
		{150, 4}, // 4.5 -> 4 (even)
		{250, 8}, // 7.5 -> 8 (even)
		{0, 0},
	}
	for _, c := range cases {
		if got := GripeFee(c.amount); got != c.want {
			t.Errorf("GripeFee(%d) = %d, want %d", c.amount, got, c.want)
		}
	}
}
