package rent

import (
	"math"
	"testing"
)

func TestRealMonthlyMatchesCalculator(t *testing.T) {
	tests := []struct {
		terms     Terms
		inflation float64
		want      float64
	}{
		// The calculator's README example.
		{Terms{Deposit: 100_000_000, Rent: 20_000_000}, 80, 19_199_742},
		// Without inflation, the real rent is the contract rent.
		{Terms{Deposit: 100_000_000, Rent: 20_000_000}, 0, 20_000_000},
		// A full deposit costs only the value the deposit loses.
		{Terms{Deposit: 1_200_000_000}, 80, 1_200_000_000 * (1 - 1/1.8) / 12},
	}
	for _, tt := range tests {
		if got := RealMonthly(tt.terms, tt.inflation, DefaultMonths); math.Abs(got-tt.want) > 1 {
			t.Errorf("RealMonthly(%+v, %v) = %.0f, want %.0f", tt.terms, tt.inflation, got, tt.want)
		}
	}
}

func TestParseAmount(t *testing.T) {
	tests := []struct {
		text string
		want int64
		ok   bool
	}{
		{"7500000000", 7_500_000_000, true},
		{"‏۲,۲۰۰,۰۰۰,۰۰۰ تومان", 2_200_000_000, true},
		{"۲٬۲۰۰٬۰۰۰ تومان", 2_200_000, true},
		{"۱٫۵ میلیارد تومان", 1_500_000_000, true},
		{"۴۵ میلیون", 45_000_000, true},
		{"رایگان", 0, true},
		{"0", 0, true},
		{"توافقی", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		if got, ok := ParseAmount(tt.text); got != tt.want || ok != tt.ok {
			t.Errorf("ParseAmount(%q) = %d, %v; want %d, %v", tt.text, got, ok, tt.want, tt.ok)
		}
	}
}

func TestFormatting(t *testing.T) {
	for got, want := range map[string]string{
		Money(19_199_742.4):  "۱۹٬۱۹۹٬۷۴۲ تومان",
		Short(7_500_000_000): "۷٫۵ میلیارد",
		Short(45_000_000):    "۴۵ میلیون",
		Short(0):             "۰",
		Percent(80):          "۸۰٪",
		Percent(45.5):        "۴۵٫۵٪",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}
