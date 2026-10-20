package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/divar"
)

func fixtureService(t *testing.T) *Service {
	t.Helper()
	s, err := NewService()
	if err != nil {
		t.Fatal(err)
	}
	s.GetFilters = func(ctx context.Context, p divar.FiltersParams) (*divar.FiltersResult, error) {
		cat, _ := json.Marshal(map[string]any{"str": map[string]string{"value": p.Category}})
		return &divar.FiltersResult{Category: p.Category, CityIDs: p.CityIDs, SearchData: divar.FilterSearchData{FormData: divar.FilterFormData{Data: divar.FilterValues{"category": cat}}}, Filters: []divar.FilterDefinition{
			{Key: "price", Title: "قیمت", ValueType: "number_range", Unit: "تومان"},
			{Key: "gearbox", Title: "گیربکس", ValueType: "str", Options: []divar.FilterOption{{Value: "automatic", Label: "اتوماتیک"}, {Value: "manual", Label: "دنده‌ای"}}},
			{Key: "brand_model", Title: "برند و مدل", ValueType: "repeated_string", LazyPayload: json.RawMessage(`{"version":"1"}`)},
		}}, nil
	}
	s.GetOptions = func(context.Context, json.RawMessage) ([]divar.LazyFilterOption, error) {
		return []divar.LazyFilterOption{{Key: "Peugeot", Label: "پژو", Children: []divar.LazyFilterOption{{Key: "Peugeot 206", Label: "پژو ۲۰۶"}}}}, nil
	}
	return s
}
func clearPlan() ai.AlertPlan {
	return ai.AlertPlan{Title: "خودرو در کرمان", Category: "light", CityIDs: []string{"13"}, AdditionalPreferences: []string{"قیمت منصفانه و مناسب خانواده باشد"}, Selections: map[string]json.RawMessage{"gearbox": json.RawMessage(`"automatic"`), "price": json.RawMessage(`{"maximum":"۱.۵ میلیارد تومان"}`), "brand_model": json.RawMessage(`["Peugeot 206"]`)}}
}

func TestCompleteRequestBuildsGroundedSearchAndCommentary(t *testing.T) {
	s := fixtureService(t)
	calls := 0
	s.PlanTurn = func(ctx context.Context, in ai.AlertTurnInput) (ai.AlertPlan, error) {
		calls++
		if in.Amounts["۱.۵ میلیارد تومان"] != "1500000000" {
			t.Error("missing exact currency conversion")
		}
		return clearPlan(), nil
	}
	draft, _ := NewDraft()
	err := s.Advance(context.Background(), draft, "پژو ۲۰۶ اتومات در کرمان تا ۱.۵ میلیارد تومان با قیمت منصفانه برای خانواده می‌خواهم")
	if err != nil {
		t.Fatal(err)
	}
	if draft.State != "confirm" || draft.QuestionsAsked != 0 || draft.Plan.Interval != 60 || draft.Query == nil || calls != 2 {
		t.Fatalf("unexpected draft %+v, calls %d", draft, calls)
	}
	if !strings.Contains(string(draft.Query.SearchData.FormData.Data["price"]), "1500000000") {
		t.Fatal("wrong monetary encoding")
	}
	if _, ok := draft.Query.SearchData.FormData.Data["family"]; ok {
		t.Fatal("subjective preference became API filter")
	}
	summary := s.Summary(draft)

	if !strings.Contains(summary, "قیمت منصفانه") || !strings.Contains(summary, "همهٔ نتیجه‌های جستجو ارسال می‌شوند") {
		t.Fatal("summary omitted commentary distinction")
	}
	// A saved draft can be reconstructed without live services or file paths.
	data, _ := json.Marshal(draft)
	var restored AlertDraft
	if err = json.Unmarshal(data, &restored); err != nil || restored.Query == nil {
		t.Fatal("draft cannot resume")
	}
}

func TestMissingCityCannotBeInventedAndDoesNotSpendQuestions(t *testing.T) {
	s := fixtureService(t)
	s.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) { return clearPlan(), nil } // Simulate a model inventing a valid city ID.
	draft, _ := NewDraft()
	if err := s.Advance(context.Background(), draft, "یک خودرو می‌خواهم"); err != nil {
		t.Fatal(err)
	}
	if draft.State != "question" || draft.Plan.Question.Kind != "city" || draft.QuestionsAsked != 0 || draft.Query != nil {
		t.Fatalf("missing city not requested: %+v", draft)
	}
	if err := s.Advance(context.Background(), draft, "کرمان"); err != nil {
		t.Fatal(err)
	}
	if draft.State != "confirm" {
		t.Fatal("city reply did not resume setup")
	}
}

func TestFiveQuestionsThenConfirmationWithGaps(t *testing.T) {
	s := fixtureService(t)
	s.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) {
		p := clearPlan()
		p.Selections = nil
		p.Question = &ai.AlertQuestion{Kind: "filter", Text: "کدام نوع را ترجیح می‌دهی؟", Options: []string{"اقتصادی", "خانوادگی", "فرقی ندارد"}}
		return p, nil
	}
	draft, _ := NewDraft()
	for i := 0; i < 5; i++ {
		if err := s.Advance(context.Background(), draft, "کرمان، هنوز مطمئن نیستم"); err != nil {
			t.Fatal(err)
		}
		if draft.State != "question" || draft.QuestionsAsked != i+1 || len(draft.Plan.Question.Options) != 3 {
			t.Fatalf("bad question %d: %+v", i, draft)
		}
	}
	if err := s.Advance(context.Background(), draft, "فرقی ندارد"); err != nil {
		t.Fatal(err)
	}
	if draft.QuestionsAsked != 5 || draft.State != "confirm" || len(draft.Plan.Gaps) == 0 {
		t.Fatal("question cap was not enforced")
	}
}

func TestCorrectionsPreserveQuestionBudgetAndChangeQuery(t *testing.T) {
	s := fixtureService(t)
	category := "light"
	city := "13"
	s.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) {
		p := clearPlan()
		p.Category = category
		p.CityIDs = []string{city}
		p.Selections = nil
		return p, nil
	}
	draft, _ := NewDraft()
	draft.QuestionsAsked = 2
	if err := s.Advance(context.Background(), draft, "خودرو در کرمان"); err != nil {
		t.Fatal(err)
	}
	before := draft.Revision
	category = "apartment-rent"
	city = "1"
	if err := s.Advance(context.Background(), draft, "نه، آپارتمان اجاره‌ای در تهران می‌خواهم"); err != nil {
		t.Fatal(err)
	}
	if draft.Plan.Category != "apartment-rent" || draft.Plan.CityIDs[0] != "1" || draft.Revision <= before || draft.QuestionsAsked != 2 {
		t.Fatal("correction lost state")
	}
	if len(draft.Query.SearchData.FormData.Data) != 1 {
		t.Fatal("old category filters survived correction")
	}
}

func TestUnknownIdentifiersAndOptionsAreRejected(t *testing.T) {
	for _, change := range []func(*ai.AlertPlan){
		func(p *ai.AlertPlan) { p.CityIDs = []string{"invented"} },
		func(p *ai.AlertPlan) { p.Category = "invented" },
		func(p *ai.AlertPlan) {
			p.Selections = map[string]json.RawMessage{"gearbox": json.RawMessage(`"invented"`)}
		},
	} {
		s := fixtureService(t)
		s.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) {
			p := clearPlan()
			change(&p)
			return p, nil
		}
		draft, _ := NewDraft()
		if err := s.Advance(context.Background(), draft, "خودرو در کرمان"); err == nil || draft.State != "error" || draft.Query != nil {
			t.Fatal("invented search data accepted")
		}
	}
}

func TestDependencyFailureRetainsInputAndRetries(t *testing.T) {
	s := fixtureService(t)
	s.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) {
		return ai.AlertPlan{}, errors.New("offline")
	}
	draft, _ := NewDraft()
	if err := s.Advance(context.Background(), draft, "خودرو در کرمان"); err == nil {
		t.Fatal("expected dependency failure")
	}
	if draft.State != "error" || draft.Messages[0].Content != "خودرو در کرمان" {
		t.Fatal("lost user input")
	}
	s.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) { return clearPlan(), nil }
	if err := s.Advance(context.Background(), draft, ""); err != nil {
		t.Fatal(err)
	}
	if len(draft.Messages) != 1 || draft.State != "confirm" {
		t.Fatal("retry duplicated or lost input")
	}
}

func TestMoneyNormalization(t *testing.T) {
	for text, want := range map[string]string{"۱.۵ میلیارد تومان": "1500000000", "۵۰۰ میلیون تومن": "500000000", "١٠٠٬٠٠٠ ریال": "10000", "۰": "0", "۱۲.۵ هزار تومان": "12500"} {
		got, err := ParseMoney(text)
		if err != nil || got != want {
			t.Fatalf("%s = %s, %v", text, got, err)
		}
	}
	if _, err := ParseMoney("پانصد؟"); err == nil {
		t.Fatal("ambiguous amount accepted")
	}
}

func TestAmbiguousCityRequiresProvinceWithoutUsingBudget(t *testing.T) {
	s := fixtureService(t)
	groups := map[string][]divar.CityReference{}
	for _, city := range s.Catalogs.Cities {
		key := cityBaseName(city.Name)
		groups[key] = append(groups[key], city)
	}
	var candidates []divar.CityReference
	for _, group := range groups {
		if len(group) > 1 && group[0].Province != group[1].Province {
			candidates = group
			break
		}
	}
	if len(candidates) < 2 {
		t.Fatal("fixture has no ambiguous cities")
	}
	chosen := candidates[0]
	s.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) {
		p := clearPlan()
		p.CityIDs = []string{chosen.ID}
		p.Selections = nil
		return p, nil
	}
	draft, _ := NewDraft()
	if err := s.Advance(context.Background(), draft, "خودرو در "+cityBaseName(chosen.Name)); err != nil {
		t.Fatal(err)
	}
	if draft.State != "question" || draft.Plan.Question.Kind != "city" || draft.QuestionsAsked != 0 || len(draft.Plan.Question.Options) != 3 {
		t.Fatal("ambiguous city guessed")
	}
	if err := s.Advance(context.Background(), draft, chosen.Name+" در استان "+chosen.Province); err != nil {
		t.Fatal(err)
	}
	if draft.State != "confirm" {
		t.Fatal("province did not disambiguate city")
	}
}

func TestQuestionLimitFallsBackToDisclosedRootCategory(t *testing.T) {
	s := fixtureService(t)
	s.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) {
		p := clearPlan()
		p.Category = ""
		p.Selections = nil
		p.Question = &ai.AlertQuestion{Kind: "filter", Text: "چه چیزی می‌خواهی؟", Options: []string{"خانه", "خودرو", "لوازم شخصی"}}
		return p, nil
	}
	draft, _ := NewDraft()
	draft.QuestionsAsked = MaxQuestions
	if err := s.Advance(context.Background(), draft, "در کرمان می‌خواهم"); err != nil {
		t.Fatal(err)
	}
	if draft.State != "confirm" || draft.Plan.Category != "ROOT" || draft.QuestionsAsked != MaxQuestions || !strings.Contains(s.Summary(draft), "همهٔ دسته‌ها") {
		t.Fatal("exhausted draft cannot reach honest broad confirmation")
	}
}

func TestKermanCarRequestAcceptsAINumericPriceBounds(t *testing.T) {
	const prompt = "ماشین مدل بالا بین یک میلیارد تا ۳ میلیارد تومن که شاستی سالم باشه و رنگ خوردگی خیلی نداشته باشه. اگهی شخصی باشه شهر کرمان"
	s := fixtureService(t)
	s.PlanTurn = func(ctx context.Context, input ai.AlertTurnInput) (ai.AlertPlan, error) {
		return ai.AlertPlan{Title: "خودروی مدل بالا در کرمان", Category: "light", CityIDs: []string{"13"}, AdditionalPreferences: []string{"مدل خودرو جدید باشد"}, Selections: map[string]json.RawMessage{
			"price":          json.RawMessage(`{"minimum":1000000000,"maximum":3000000000}`),
			"chassis_status": json.RawMessage(`"both-healthy"`),
			"body_status":    json.RawMessage(`["intact","some-scratches"]`),
			"business-type":  json.RawMessage(`["personal"]`),
		}}, nil
	}
	s.GetFilters = func(ctx context.Context, p divar.FiltersParams) (*divar.FiltersResult, error) {
		return &divar.FiltersResult{Category: p.Category, CityIDs: p.CityIDs, SearchData: divar.FilterSearchData{FormData: divar.FilterFormData{Data: divar.FilterValues{"category": json.RawMessage(`{"str":{"value":"light"}}`)}}}, Filters: []divar.FilterDefinition{
			{Key: "price", Title: "قیمت", ValueType: "number_range", Unit: "تومان", Validators: json.RawMessage(`{"minimum_validators":[{"min":{}}],"maximum_validators":[{"max":{"max":"10000000000000"}}]}`)},
			{Key: "chassis_status", Title: "وضعیت شاسی", ValueType: "str", Options: []divar.FilterOption{{Value: "both-healthy", Label: "هر دو سالم و پلمب"}}},
			{Key: "body_status", Title: "وضعیت بدنه", ValueType: "repeated_string", Options: []divar.FilterOption{{Value: "intact", Label: "سالم و بی‌خط و خش"}, {Value: "some-scratches", Label: "خط و خش جزیی"}}},
			{Key: "business-type", Title: "آگهی‌دهنده", ValueType: "repeated_string", Options: []divar.FilterOption{{Value: "personal", Label: "شخصی"}}},
		}}, nil
	}
	draft, _ := NewDraft()
	if err := s.Advance(context.Background(), draft, prompt); err != nil {
		t.Fatal(err)
	}
	if draft.State != "confirm" || draft.Query == nil || draft.Plan.CityIDs[0] != "13" {
		t.Fatalf("failed draft: %+v", draft)
	}
	form := draft.Query.SearchData.FormData.Data
	if string(form["price"]) != `{"number_range":{"minimum":"1000000000","maximum":"3000000000"}}` {
		t.Fatalf("price lost precision: %s", form["price"])
	}
	for _, key := range []string{"chassis_status", "body_status", "business-type"} {
		if len(form[key]) == 0 {
			t.Fatalf("missing stated filter %s", key)
		}
	}
}

func TestUnsupportedCarTypeIsRepairedUsingActualFilterKeys(t *testing.T) {
	s := fixtureService(t)
	repaired := false
	s.PlanTurn = func(ctx context.Context, input ai.AlertTurnInput) (ai.AlertPlan, error) {
		p := clearPlan()
		p.Selections = map[string]json.RawMessage{"price": json.RawMessage(`{"minimum":1000000000,"maximum":3000000000}`), "car_type": json.RawMessage(`"sedan"`)}
		if input.SchemaAvailable && input.ValidationError != "" {
			if !strings.Contains(input.ValidationError, "car_type") || !strings.Contains(input.ValidationError, "price") {
				t.Fatal("repair lacks real schema feedback")
			}
			repaired = true
			delete(p.Selections, "car_type")
			p.AdditionalPreferences = []string{"خودرو سواری سدان باشد"}
		}
		return p, nil
	}
	draft, _ := NewDraft()
	if err := s.Advance(context.Background(), draft, "خودرو سواری سدان بین یک تا سه میلیارد تومان در کرمان می‌خواهم"); err != nil {
		t.Fatal(err)
	}
	if !repaired || draft.State != "confirm" {
		t.Fatal("unsupported field did not trigger a correction")
	}
	if _, ok := draft.Query.SearchData.FormData.Data["car_type"]; ok {
		t.Fatal("invented filter reached search")
	}
	if !strings.Contains(strings.Join(draft.Plan.AdditionalPreferences, " "), "سدان") {
		t.Fatal("preference lost during repair")
	}
}

func TestPersistentUnsupportedFilterBecomesDisclosedCommentary(t *testing.T) {
	s := fixtureService(t)
	s.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) {
		p := clearPlan()
		p.Selections = map[string]json.RawMessage{"price": json.RawMessage(`{"minimum":1000000000,"maximum":3000000000}`), "car_type": json.RawMessage(`"sedan"`)}
		return p, nil
	}
	draft, _ := NewDraft()
	if err := s.Advance(context.Background(), draft, "ماشین در کرمان"); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(context.Background(), draft, "نوع سواری سدان باشد"); err != nil {
		t.Fatal(err)
	}
	if draft.State != "confirm" || draft.Query == nil {
		t.Fatal("unsupported field still breaks the conversation")
	}
	form := draft.Query.SearchData.FormData.Data
	if _, ok := form["car_type"]; ok {
		t.Fatal("unsupported filter was sent to Divar")
	}
	if len(form["price"]) == 0 {
		t.Fatal("valid filters were discarded")
	}
	summary := s.Summary(draft)
	if !strings.Contains(summary, "سواری سدان") || !strings.Contains(summary, "در جستجو محدود نشده‌اند") {
		t.Fatal("fallback silently discarded user preference")
	}
	if strings.Contains(summary, "car_type") {
		t.Fatal("technical API key leaked into chat")
	}
}
