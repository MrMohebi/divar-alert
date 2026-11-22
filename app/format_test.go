package app

import (
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"github.com/mrmohebi/divar-alert/platform"
)

func TestRichTextBoldsMarkedPhrases(t *testing.T) {
	tests := map[string]string{
		"قیمت **۲۰ میلیون تومان** است":   "قیمت " + bold("۲۰ میلیون تومان") + " است",
		"ستاره‌ی **تنها":                 "ستاره‌ی تنها",
		"بدون قالب":                      "بدون قالب",
		"نشانه‌ی جعلی \uE000باز":         "نشانه‌ی جعلی باز",
		"**پارکینگ** و **آسانسور** دارد": bold("پارکینگ") + " و " + bold("آسانسور") + " دارد",
	}
	for in, want := range tests {
		if got := richText(in); got != want {
			t.Errorf("richText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderPerPlatform(t *testing.T) {
	text := "قیمت" + bold("۲۰ میلیون") + "، <b>&\n" + bold("خوب")
	telegram, mode := render(platform.Telegram, text)
	if mode != models.ParseModeHTML || telegram != "قیمت<b>۲۰ میلیون</b>، &lt;b&gt;&amp;\n<b>خوب</b>" {
		t.Fatalf("telegram = %q (%s)", telegram, mode)
	}
	bale, mode := render(platform.Bale, text)
	if mode != "" || bale != "قیمت *۲۰ میلیون* ، <b>&\n*خوب*" {
		t.Fatalf("bale = %q (%s)", bale, mode)
	}
}

func TestBalanceBoldAcrossSplitParts(t *testing.T) {
	parts := balanceBold(splitText("اول "+bold(strings.Repeat("کلمه ", 40))+" آخر", 100))
	if len(parts) < 2 {
		t.Fatalf("parts = %q", parts)
	}
	for _, part := range parts {
		if strings.Count(part, string(boldOpen)) != strings.Count(part, string(boldClose)) {
			t.Fatalf("unbalanced part %q", part)
		}
	}
	if !strings.HasPrefix(parts[1], string(boldOpen)) {
		t.Fatalf("bold not reopened: %q", parts[1])
	}
}

func TestListingTextShowsAnalysisAsBulletPoints(t *testing.T) {
	evaluation := &ai.EvaluationResult{
		Score:       82,
		Decision:    "**ارزش تماس دارد**",
		Strengths:   []string{"اجاره‌ی واقعی **۱۹ میلیون** کمتر از آگهی‌های مشابه است", "**پارکینگ** دارد"},
		Concerns:    []string{"سال ساخت مشخص نیست"},
		Explanation: "در مجموع **مناسب** است.",
	}
	post := divar.Listing{Token: "abc", Title: "آپارتمان", TopDescriptionText: "ودیعه: ۱۰۰,۰۰۰,۰۰۰ تومان"}
	text := listingText(db.Alert{Title: "اجاره", AIEnabled: true}, post, nil, evaluation)
	for _, want := range []string{
		bold("آپارتمان"),
		"ودیعه: " + bold("۱۰۰,۰۰۰,۰۰۰ تومان"),
		"امتیاز: " + bold("82 از ۱۰۰"),
		"پیشنهاد: " + bold("ارزش تماس دارد"),
		bold("✅ نقاط قوت") + "\n• اجاره‌ی واقعی " + bold("۱۹ میلیون") + " کمتر از آگهی‌های مشابه است\n• " + bold("پارکینگ") + " دارد\n",
		bold("⚠️ نکات قابل توجه") + "\n• سال ساخت مشخص نیست\n",
		"در مجموع " + bold("مناسب") + " است.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text lacks %q:\n%s", plainText(want), plainText(text))
		}
	}
	// Without points the sections are left out.
	if text := listingText(db.Alert{AIEnabled: true}, post, nil, &ai.EvaluationResult{Score: 50, Decision: "بررسی کن", Explanation: "معمولی است."}); strings.Contains(text, "نقاط قوت") || strings.Contains(text, "نکات قابل توجه") {
		t.Fatalf("empty sections shown:\n%s", plainText(text))
	}
}
