// Package rent calculates the real monthly cost of a rental in today's money,
// following https://github.com/MrMohebi/actual-rent-calculator
// (https://rent.m17i.xyz). With inflation, the deposit returned at the end of
// the lease and later rent payments are both worth less than they look.
package rent

import (
	"math"
	"strconv"
	"strings"

	"github.com/mrmohebi/divar-alert/divar"
)

// Calculator defaults.
const (
	DefaultInflation = 80 // yearly inflation, percent
	DefaultMonths    = 12 // contract length
)

// Terms are a lease's deposit (ودیعه) and monthly rent, in toman.
type Terms struct {
	Deposit int64
	Rent    int64
}

// RealMonthly returns the real monthly cost in today's toman for a contract
// of months, rent paid at the start of each month, under yearly inflation
// in percent:
//
//	m = (1 + inflation)^(1/12) − 1
//	real monthly = (Σ rent ÷ (1+m)^k for k = 0..months−1 + deposit − deposit ÷ (1+m)^months) ÷ months
func RealMonthly(terms Terms, inflation float64, months int) float64 {
	if months < 1 {
		months = DefaultMonths
	}
	deposit := math.Max(0, float64(terms.Deposit))
	rent := math.Max(0, float64(terms.Rent))
	m := math.Pow(1+inflation/100, 1.0/12) - 1
	discounted := 0.0
	for k := range months {
		discounted += 1 / math.Pow(1+m, float64(k))
	}
	depositLoss := deposit - deposit/math.Pow(1+m, float64(months))
	return (rent*discounted + depositLoss) / float64(months)
}

// ParseAmount reads a Divar money value such as "7500000000",
// "‏۲,۲۰۰,۰۰۰,۰۰۰ تومان", "۱٫۵ میلیارد تومان" or "رایگان" (free, 0).
// Negotiable or unreadable values report false.
func ParseAmount(text string) (int64, bool) {
	text = strings.TrimSpace(divar.NormalizePersian(text))
	if text == "" {
		return 0, false
	}
	if strings.Contains(text, "رایگان") || strings.Contains(text, "مجانی") {
		return 0, true
	}
	multiplier := 1.0
	for _, unit := range []struct {
		name   string
		factor float64
	}{{"میلیارد", 1e9}, {"میلیون", 1e6}, {"هزار", 1e3}} {
		if strings.Contains(text, unit.name) {
			multiplier = unit.factor
			text = strings.ReplaceAll(text, unit.name, "")
			break
		}
	}
	text = strings.NewReplacer("تومان", "", "تومن", "", " ", "", ",", "", "٬", "", "٫", ".").Replace(text)
	if multiplier == 1 {
		// Without a unit, a dot can only be a thousands separator.
		text = strings.ReplaceAll(text, ".", "")
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value < 0 || math.IsInf(value, 0) {
		return 0, false
	}
	value = math.Round(value * multiplier)
	if value > math.MaxInt64/2 {
		return 0, false
	}
	return int64(value), true
}

// Money formats an amount in toman with Persian digits: "۱۹٬۱۹۹٬۷۴۲ تومان".
func Money(n float64) string {
	return persianDigits(group(int64(math.Round(n)))) + " تومان"
}

// Short formats an amount compactly with Persian digits: "۱٫۵ میلیارد".
func Short(n int64) string {
	switch {
	case n >= 1e9:
		return persianDigits(trimDecimal(float64(n)/1e9, 2)) + " میلیارد"
	case n >= 1e6:
		return persianDigits(trimDecimal(float64(n)/1e6, 1)) + " میلیون"
	case n >= 1e3:
		return persianDigits(group(int64(math.Round(float64(n)/1e3)))) + " هزار"
	}
	return persianDigits(strconv.FormatInt(n, 10))
}

// Percent formats a percentage with Persian digits: "۸۰٪".
func Percent(p float64) string {
	return persianDigits(trimDecimal(p, 1)) + "٪"
}

func group(n int64) string {
	digits := strconv.FormatInt(n, 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var out strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteString(",")
		}
		out.WriteRune(d)
	}
	return sign + out.String()
}

func trimDecimal(v float64, places int) string {
	s := strconv.FormatFloat(v, 'f', places, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func persianDigits(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9':
			return '۰' + r - '0'
		case r == ',':
			return '٬'
		case r == '.':
			return '٫'
		}
		return r
	}, s)
}
