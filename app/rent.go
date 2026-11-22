package app

import (
	"fmt"
	"strings"

	"github.com/mrmohebi/divar-alert/divar"
	"github.com/mrmohebi/divar-alert/rent"
)

// rentQuote is a rental listing's real monthly cost in today's money.
type rentQuote struct {
	Terms     rent.Terms
	Real      float64
	Inflation float64
	// Converted is the alternative deposit/rent split of a convertible
	// listing, when it offers one.
	Converted     *rent.Terms
	ConvertedReal float64
}

// quoteRent prices a rental from its details, or from the search card when
// details are unavailable. It returns nil for anything that is not a monthly
// rental with known terms.
func quoteRent(details divar.PostImportantData, post divar.Listing, inflation float64) *rentQuote {
	terms, ok := rentTerms(details.Price.Deposit, details.Price.Rent)
	if !ok {
		terms, ok = listingRentTerms(post)
	}
	if !ok {
		return nil
	}
	quote := &rentQuote{Terms: terms, Real: rent.RealMonthly(terms, inflation, rent.DefaultMonths), Inflation: inflation}
	if details.Price.TransDeposit != "" && details.Price.TransRent != "" {
		if converted, ok := rentTerms(details.Price.TransDeposit, details.Price.TransRent); ok && converted != terms {
			quote.Converted = &converted
			quote.ConvertedReal = rent.RealMonthly(converted, inflation, rent.DefaultMonths)
		}
	}
	return quote
}

// rentTerms parses a deposit and monthly rent. A deposit is required, which
// tells monthly rentals apart from daily ones; a missing rent means a full
// deposit (رهن کامل).
func rentTerms(depositText, rentText string) (rent.Terms, bool) {
	deposit, ok := rent.ParseAmount(depositText)
	if !ok {
		return rent.Terms{}, false
	}
	var monthly int64
	if strings.TrimSpace(rentText) != "" {
		if monthly, ok = rent.ParseAmount(rentText); !ok {
			return rent.Terms{}, false
		}
	}
	if deposit == 0 && monthly == 0 {
		return rent.Terms{}, false
	}
	return rent.Terms{Deposit: deposit, Rent: monthly}, true
}

// listingRentTerms reads search card lines such as "ودیعه: ۱,۰۰۰,۰۰۰,۰۰۰ تومان",
// "اجاره: ۱۰,۰۰۰,۰۰۰ تومان" and "رهن کامل".
func listingRentTerms(post divar.Listing) (rent.Terms, bool) {
	var depositText, rentText string
	for _, line := range []string{post.TopDescriptionText, post.MiddleDescriptionText, post.BottomDescriptionText} {
		line = divar.NormalizePersian(line)
		label, value, found := strings.Cut(line, ":")
		switch {
		case found && strings.TrimSpace(label) == "ودیعه":
			depositText = value
		case found && strings.TrimSpace(label) == "اجاره":
			rentText = value
		case line == "رهن کامل":
			rentText = "0"
		}
	}
	return rentTerms(depositText, rentText)
}

// text is the real-rent section of a listing message.
func (q *rentQuote) text() string {
	var text strings.Builder
	text.WriteString(bold("اجاره‌ی واقعی ماهانه: "+rent.Money(q.Real)) + "\n")
	fmt.Fprintf(&text, "(به پول امروز، با تورم سالانه‌ی %s در قرارداد %s ماهه)\n", rent.Percent(q.Inflation), rent.Short(rent.DefaultMonths))
	if c := q.Converted; c != nil {
		fmt.Fprintf(&text, "در حالت تبدیل (ودیعه %s، اجاره %s): %s\n", rent.Short(c.Deposit), rent.Short(c.Rent), bold(rent.Money(q.ConvertedReal)))
	}
	return text.String()
}
