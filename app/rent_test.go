package app

import (
	"strings"
	"testing"

	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"github.com/mrmohebi/divar-alert/rent"
)

func TestQuoteRent(t *testing.T) {
	tests := []struct {
		name      string
		price     divar.RentalPriceInfo
		post      divar.Listing
		want      *rent.Terms
		converted *rent.Terms
	}{
		{
			name:      "convertible slider",
			price:     divar.RentalPriceInfo{Deposit: "7500000000", Rent: "1000000", TransDeposit: "6000000000", TransRent: "45000000"},
			want:      &rent.Terms{Deposit: 7_500_000_000, Rent: 1_000_000},
			converted: &rent.Terms{Deposit: 6_000_000_000, Rent: 45_000_000},
		},
		{
			name:  "fixed rows, full deposit",
			price: divar.RentalPriceInfo{Deposit: "‏۲,۲۰۰,۰۰۰,۰۰۰ تومان", Rent: "رایگان"},
			want:  &rent.Terms{Deposit: 2_200_000_000},
		},
		{
			name: "search card when details are missing",
			post: divar.Listing{TopDescriptionText: "ودیعه: ۱,۰۰۰,۰۰۰,۰۰۰ تومان", MiddleDescriptionText: "اجاره: ۱۰,۰۰۰,۰۰۰ تومان"},
			want: &rent.Terms{Deposit: 1_000_000_000, Rent: 10_000_000},
		},
		{
			name: "search card full deposit",
			post: divar.Listing{TopDescriptionText: "ودیعه: ۱,۴۰۰,۰۰۰,۰۰۰ تومان", MiddleDescriptionText: "رهن کامل"},
			want: &rent.Terms{Deposit: 1_400_000_000},
		},
		{name: "negotiable", price: divar.RentalPriceInfo{Deposit: "توافقی", Rent: "توافقی"}},
		{name: "rent without a deposit (daily rental)", price: divar.RentalPriceInfo{Rent: "3000000"}},
		{name: "not a rental", post: divar.Listing{TopDescriptionText: "۵۰۰,۰۰۰,۰۰۰ تومان"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quote := quoteRent(divar.PostImportantData{Price: tt.price}, tt.post, 80)
			if tt.want == nil {
				if quote != nil {
					t.Fatalf("quote = %+v, want none", quote)
				}
				return
			}
			if quote == nil || quote.Terms != *tt.want || quote.Real != rent.RealMonthly(*tt.want, 80, 12) {
				t.Fatalf("quote = %+v, want terms %+v", quote, *tt.want)
			}
			if (quote.Converted == nil) != (tt.converted == nil) || (tt.converted != nil && *quote.Converted != *tt.converted) {
				t.Fatalf("converted = %+v, want %+v", quote.Converted, tt.converted)
			}
		})
	}
}

func TestListingTextShowsRealRentBeforeAnalysis(t *testing.T) {
	quote := quoteRent(divar.PostImportantData{Price: divar.RentalPriceInfo{Deposit: "100000000", Rent: "20000000"}}, divar.Listing{}, 80)
	evaluation := &ai.EvaluationResult{Score: 70, Decision: "ارزش دیدن دارد", Explanation: "قیمت مناسب است."}
	text := plainText(listingText(db.Alert{Title: "اجاره", AIEnabled: true}, divar.Listing{Token: "abc", Title: "آپارتمان ۷۰ متری"}, quote, evaluation))
	realRent := strings.Index(text, "اجاره‌ی واقعی ماهانه: ۱۹٬۱۹۹٬۷۴۲ تومان")
	if realRent < 0 || !strings.Contains(text, "تورم سالانه‌ی ۸۰٪") {
		t.Fatalf("real rent missing:\n%s", text)
	}
	if title, analysis := strings.Index(text, "آپارتمان ۷۰ متری"), strings.Index(text, "تحلیل آگهی"); !(title < realRent && realRent < analysis) {
		t.Fatalf("real rent should follow the listing and precede the analysis:\n%s", text)
	}
}
