// Package conversation owns the resumable, transport-independent alert wizard.
package conversation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/divar"
)

const ProcessKey = "NATURAL_ALERT"
const MaxQuestions = 5

type AlertDraft struct {
	ID             string             `json:"id"`
	Revision       int                `json:"revision"`
	State          string             `json:"state"` // prompt, question, confirm, edit, error
	QuestionsAsked int                `json:"questions_asked"`
	OriginalPrompt string             `json:"original_prompt"`
	Messages       []ai.Message       `json:"messages"`
	Plan           *ai.AlertPlan      `json:"plan,omitempty"`
	Query          *divar.SearchQuery `json:"query,omitempty"`
	Applied        []string           `json:"applied,omitempty"`
}

type Service struct {
	Catalogs   *divar.Catalogs
	PlanTurn   func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error)
	GetFilters func(context.Context, divar.FiltersParams) (*divar.FiltersResult, error)
	GetOptions func(context.Context, json.RawMessage) ([]divar.LazyFilterOption, error)
}

func NewService() (*Service, error) {
	catalogs, err := divar.LoadCatalogs()
	if err != nil {
		return nil, err
	}
	return &Service{catalogs, ai.PlanAlertTurn, divar.GetFilters, divar.GetLazyFilterOptions}, nil
}

func NewDraft() (*AlertDraft, error) {
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	return &AlertDraft{ID: hex.EncodeToString(id[:]), Revision: 1, State: "prompt"}, nil
}

// Advance mutates only the draft in memory. The transport persists it before
// replying, including user input on dependency errors so Retry is lossless.
func (s *Service) Advance(ctx context.Context, d *AlertDraft, text string) error {
	d.Revision++
	d.Query = nil
	if text != "" {
		if len([]rune(text)) > 4000 {
			return fmt.Errorf("message exceeds 4000 characters")
		}
		if d.OriginalPrompt == "" {
			d.OriginalPrompt = text
		}
		d.Messages = append(d.Messages, ai.Message{Role: "user", Content: text})
	}
	// Bound persisted input without discarding the opening description or latest answers.
	if len(d.Messages) > 40 {
		return fmt.Errorf("conversation is too long; start a new draft")
	}
	fail := func(err error) error { d.State = "error"; return err }
	input := s.input(d)
	plan, err := s.plan(ctx, input)
	if err != nil {
		return fail(err)
	}
	if err = s.validateIdentity(plan); err != nil {
		return fail(err)
	}
	if plan.Interval == 0 {
		plan.Interval = 60
	}
	if plan.Interval < 10 || plan.Interval > 86400 {
		plan.Interval = 60
		plan.Gaps = append(plan.Gaps, "زمان درخواستی پشتیبانی نمی‌شود؛ فعلاً هر یک دقیقه بررسی می‌کنم.")
	}
	// A city must appear in an actual user answer; a valid catalog ID alone
	// is not evidence that the user selected it.
	if len(plan.CityIDs) > 0 {
		if question := s.cityClarification(plan.CityIDs, d.Messages); question != nil {
			plan.CityIDs = nil
			plan.Question = question
		}
	}
	// Location clarification always takes precedence and is outside the question cap.
	if len(plan.CityIDs) == 0 || (plan.Question != nil && plan.Question.Kind == "city") {
		if plan.Question == nil || plan.Question.Kind != "city" {
			plan.Question = &ai.AlertQuestion{Kind: "city", Text: "برای کدام شهر دنبال آگهی هستی؟ اسم شهر را بنویس."}
		}
		return s.ask(d, plan)
	}
	if plan.Category == "" {
		if d.QuestionsAsked < MaxQuestions {
			if plan.Question == nil {
				return fail(fmt.Errorf("missing category clarification"))
			}
			return s.ask(d, plan)
		}
		plan.Category = "ROOT"
		plan.Question = nil
		plan.Gaps = append(plan.Gaps, "دسته‌بندی هنوز مشخص نیست؛ فعلاً همهٔ دسته‌ها را در نظر گرفتم.")
	}
	schema, err := s.GetFilters(ctx, divar.FiltersParams{Category: plan.Category, CityIDs: plan.CityIDs})
	if err != nil {
		return fail(err)
	}
	// A fresh schema prevents obsolete filters from surviving a category/city change.
	for i := range schema.Filters {
		f := &schema.Filters[i]
		if len(f.LazyPayload) == 0 {
			continue
		}
		options, e := s.GetOptions(ctx, f.LazyPayload)
		if e != nil {
			// Keep the wish for commentary rather than fabricate a lazy option value.
			f.Options = nil
			continue
		}
		f.Options = convertOptions(options)
	}
	hostGaps := append([]string(nil), plan.Gaps...)
	input.Current = &plan
	input.SchemaAvailable = true
	input.Filters = plannerFilters(schema.Filters, d.Messages)
	plan, err = s.plan(ctx, input)
	if err != nil {
		return fail(err)
	}
	if err = s.validateIdentity(plan); err != nil {
		return fail(err)
	}
	if d.QuestionsAsked >= MaxQuestions && plan.Category == "" {
		plan.Category = schema.Category
	}
	for _, gap := range hostGaps {
		found := false
		for _, existing := range plan.Gaps {
			if existing == gap {
				found = true
			}
		}
		if !found {
			plan.Gaps = append(plan.Gaps, gap)
		}
	}
	if plan.Category != schema.Category || !sameIDs(plan.CityIDs, schema.CityIDs) {
		return fail(fmt.Errorf("planner changed identity after schema lookup"))
	}
	if plan.Interval == 0 {
		plan.Interval = 60
	}
	if plan.Interval < 10 || plan.Interval > 86400 {
		plan.Interval = 60
		plan.Gaps = append(plan.Gaps, "زمان درخواستی پشتیبانی نمی‌شود؛ فعلاً هر یک دقیقه بررسی می‌کنم.")
	}
	if plan.Question != nil && plan.Question.Kind == "city" {
		return s.ask(d, plan)
	}
	if plan.Question != nil && d.QuestionsAsked < MaxQuestions {
		return s.ask(d, plan)
	}
	if plan.Question != nil {
		plan.Gaps = append(plan.Gaps, "بعضی جزئیات هنوز روشن نیست؛ پیش از تأیید می‌توانی آن‌ها را اصلاح کنی.")
		plan.Question = nil
	}
	selections := map[string]any{}
	applied := []string{}
	byKey := map[string]divar.FilterDefinition{}
	for _, f := range schema.Filters {
		byKey[f.Key] = f
	}
	keys := make([]string, 0, len(plan.Selections))
	for key := range plan.Selections {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		f, ok := byKey[key]
		if !ok {
			return fail(fmt.Errorf("unknown proposed filter %q", key))
		}
		// Lazy options are mandatory for validating a model-supplied ID.
		if len(f.LazyPayload) > 0 && len(f.Options) == 0 {
			plan.AdditionalPreferences = append(plan.AdditionalPreferences, "خواستهٔ کاربر دربارهٔ «"+f.Title+"» را در امتیاز و توضیح بررسی کن: "+d.OriginalPrompt)
			plan.Gaps = append(plan.Gaps, "گزینه‌های «"+f.Title+"» فعلاً در دسترس نیست؛ در جستجو محدود نشده است.")
			delete(plan.Selections, key)
			continue
		}
		if f.ValueType == "number_range" {
			var bounds divar.FilterNumberRange
			if e := json.Unmarshal(plan.Selections[key], &bounds); e != nil {
				return fail(e)
			}
			for _, bound := range []*string{&bounds.Minimum, &bounds.Maximum} {
				if *bound == "" {
					continue
				}
				if strings.Contains(f.Unit, "تومان") {
					value, e := ParseMoney(*bound)
					if e != nil {
						return fail(e)
					}
					*bound = value
				} else {
					*bound = strings.ReplaceAll(divar.NormalizePersian(*bound), ",", "")
				}
			}
			plan.Selections[key], _ = json.Marshal(bounds)
		}
		v, e := f.DecodeSelection(plan.Selections[key])
		if e != nil {
			return fail(fmt.Errorf("filter %s: %w", key, e))
		}
		selections[key] = v
		applied = append(applied, describeSelection(f, v))
	}
	form, err := schema.BuildFormData(selections)
	if err != nil {
		return fail(err)
	}
	query := divar.SearchQuery{CityIDs: append([]string(nil), plan.CityIDs...), SearchData: schema.SearchData}
	query.SearchData.FormData.Data = form
	query.SearchData.Query = plan.Keywords
	d.Plan = &plan
	d.Query = &query
	d.Applied = applied
	d.State = "confirm"
	return nil
}

func (s *Service) plan(ctx context.Context, input ai.AlertTurnInput) (ai.AlertPlan, error) {
	var last error
	allowed := map[string]bool{}
	for _, field := range input.Filters {
		allowed[field.Key] = true
	}
	allowedKeys := make([]string, 0, len(allowed))
	for key := range allowed {
		allowedKeys = append(allowedKeys, key)
	}
	sort.Strings(allowedKeys)
	for attempt := 0; attempt < 2; attempt++ {
		p, err := s.PlanTurn(ctx, input)
		if err != nil {
			return ai.AlertPlan{}, err
		}
		if err = s.validateIdentity(p); err != nil {
			last = err
			input.ValidationError = err.Error()
			continue
		}
		var unsupported []string
		if input.SchemaAvailable {
			for key := range p.Selections {
				if !allowed[key] {
					unsupported = append(unsupported, key)
				}
			}
		}
		if len(unsupported) == 0 {
			return p, nil
		}
		sort.Strings(unsupported)
		last = fmt.Errorf("unsupported filter keys %q; the ONLY allowed filter keys are %q. Remove unsupported keys from selections. Preserve the user's corresponding wishes in additional_preferences in Persian, for scoring/commentary only", unsupported, allowedKeys)
		input.ValidationError = last.Error()
		if attempt == 1 {
			// A model's invented API field must not break an otherwise valid draft.
			// Preserve all user answers for commentary instead of silently dropping a wish.
			selections := make(map[string]json.RawMessage, len(p.Selections))
			for key, value := range p.Selections {
				if allowed[key] {
					selections[key] = value
				}
			}
			p.Selections = selections
			var userText []string
			for _, message := range input.Messages {
				if message.Role == "user" {
					userText = append(userText, message.Content)
				}
			}
			p.AdditionalPreferences = append(p.AdditionalPreferences, "در امتیاز و توضیح آگهی، جزئیات خواسته و پاسخ‌های کاربر را بررسی کن: "+strings.Join(userText, "؛ "))
			p.Gaps = append(p.Gaps, "بعضی ترجیح‌ها در جستجو محدود نشده‌اند و فقط در امتیاز و توضیح آگهی بررسی می‌شوند.")
			return p, nil
		}
	}
	return ai.AlertPlan{}, last
}

func (s *Service) validateIdentity(p ai.AlertPlan) error {
	if err := ai.ValidateAlertPlan(p); err != nil {
		return err
	}
	if p.Category != "" {
		if _, ok := s.Catalogs.Categories[p.Category]; !ok {
			return fmt.Errorf("unknown category %q", p.Category)
		}
	}
	seen := map[string]bool{}
	for _, id := range p.CityIDs {
		if _, ok := s.Catalogs.Cities[id]; !ok || seen[id] {
			return fmt.Errorf("unknown or duplicate city %q", id)
		}
		seen[id] = true
	}
	return nil
}

func (s *Service) ask(d *AlertDraft, p ai.AlertPlan) error {
	if p.Question == nil {
		return fmt.Errorf("question missing")
	}
	if err := ai.ValidateAlertPlan(p); err != nil {
		d.State = "error"
		return err
	}
	if p.Question.Kind == "filter" {
		if d.QuestionsAsked >= MaxQuestions {
			return fmt.Errorf("question budget exhausted")
		}
		d.QuestionsAsked++
	}
	d.Plan = &p
	d.State = "question"
	d.Messages = append(d.Messages, ai.Message{Role: "assistant", Content: p.Question.Text + "\n" + strings.Join(p.Question.Options, " | ")})
	return nil
}

func (s *Service) input(d *AlertDraft) ai.AlertTurnInput {
	remaining := MaxQuestions - d.QuestionsAsked
	if remaining < 0 {
		remaining = 0
	}
	input := ai.AlertTurnInput{Current: d.Plan, QuestionsRemaining: remaining}
	for _, m := range d.Messages {
		input.Messages = append(input.Messages, ai.Message{Role: m.Role, Content: divar.NormalizePersian(m.Content)})
	}
	rawMessages := []string{}
	for _, m := range d.Messages {
		if m.Role == "user" {
			rawMessages = append(rawMessages, m.Content)
		}
	}
	input.Amounts = moneyMentions(rawMessages)
	ids := make([]string, 0, len(s.Catalogs.Cities))
	for id := range s.Catalogs.Cities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var cities strings.Builder
	for _, id := range ids {
		c := s.Catalogs.Cities[id]
		fmt.Fprintf(&cities, "%s|%s|%s\n", id, c.Name, c.Province)
	}
	input.Cities = cities.String()
	slugs := make([]string, 0, len(s.Catalogs.Categories))
	for slug := range s.Catalogs.Categories {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	var cats strings.Builder
	for _, slug := range slugs {
		c := s.Catalogs.Categories[slug]
		fmt.Fprintf(&cats, "%s|%s|%s\n", slug, c.Name, c.ParentSlug)
	}
	input.Categories = cats.String()
	return input
}

func normalizedWords(text string) []string {
	return strings.FieldsFunc(divar.NormalizePersian(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
func containsName(words []string, name string) bool {
	needle := strings.Join(normalizedWords(name), "")
	for i := range words {
		candidate := ""
		for j := i; j < len(words) && len([]rune(candidate)) <= len([]rune(needle)); j++ {
			candidate += words[j]
			if candidate == needle {
				return true
			}
		}
	}
	return false
}
func cityBaseName(name string) string { return strings.TrimSpace(strings.Split(name, "(")[0]) }
func (s *Service) cityClarification(ids []string, messages []ai.Message) *ai.AlertQuestion {
	var text strings.Builder
	for _, m := range messages {
		if m.Role == "user" {
			text.WriteString(" " + m.Content)
		}
	}
	words := normalizedWords(strings.ReplaceAll(text.String(), "تهرون", "تهران"))
	for _, id := range ids {
		city := s.Catalogs.Cities[id]
		name := cityBaseName(city.Name)
		if !containsName(words, name) {
			return &ai.AlertQuestion{Kind: "city", Text: "برای کدام شهر دنبال آگهی هستی؟ اسم شهر را بنویس."}
		}
		var candidates []divar.CityReference
		for _, c := range s.Catalogs.Cities {
			if strings.Join(normalizedWords(cityBaseName(c.Name)), "") == strings.Join(normalizedWords(name), "") {
				candidates = append(candidates, c)
			}
		}
		if len(candidates) > 1 && !containsName(words, city.Province) {
			sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
			return &ai.AlertQuestion{Kind: "city", Text: "چند شهر با نام «" + name + "» وجود دارد. کدام را می‌خواهی؟", Options: []string{candidates[0].Name + " در استان " + candidates[0].Province, candidates[1].Name + " در استان " + candidates[1].Province, "شهر دیگری مدنظر من است"}}
		}
	}
	return nil
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, ",") == strings.Join(y, ",")
}
func convertOptions(options []divar.LazyFilterOption) []divar.FilterOption {
	var out []divar.FilterOption
	for _, o := range options {
		out = append(out, divar.FilterOption{Value: o.Key, Label: o.Label, Children: convertOptions(o.Children)})
	}
	return out
}

// Limit large lazy trees in the model context, while retaining the complete tree
// locally for strict selection validation. Rank exact product mentions first.
func plannerFilters(filters []divar.FilterDefinition, messages []ai.Message) []divar.FilterDefinition {
	text := ""
	for _, m := range messages {
		if m.Role == "user" {
			text += " " + m.Content
		}
	}
	text = strings.ReplaceAll(divar.NormalizePersian(text), " ", "")
	out := append([]divar.FilterDefinition(nil), filters...)
	for i := range out {
		if len(out[i].LazyPayload) == 0 {
			continue
		}
		type candidate struct {
			o     divar.FilterOption
			score int
		}
		var candidates []candidate
		var walk func([]divar.FilterOption, int)
		walk = func(options []divar.FilterOption, parentScore int) {
			for _, o := range options {
				score := parentScore
				for _, label := range []string{o.Label, o.Value} {
					needle := strings.ReplaceAll(divar.NormalizePersian(label), " ", "")
					if needle != "" && strings.Contains(text, needle) {
						score += len([]rune(needle)) * 10
					}
				}
				flat := o
				flat.Children = nil
				candidates = append(candidates, candidate{flat, score})
				walk(o.Children, score/2)
			}
		}
		walk(out[i].Options, 0)
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
		out[i].Options = nil
		for j, c := range candidates {
			if j >= 100 {
				break
			}
			out[i].Options = append(out[i].Options, c.o)
		}
	}
	return out
}

func describeSelection(f divar.FilterDefinition, v any) string {
	labels := map[string]string{}
	var walk func([]divar.FilterOption)
	walk = func(opts []divar.FilterOption) {
		for _, o := range opts {
			labels[o.Value] = o.Label
			walk(o.Children)
		}
	}
	walk(f.Options)
	label := func(v string) string {
		if s := labels[v]; s != "" {
			return s
		}
		return v
	}
	switch x := v.(type) {
	case bool:
		if x {
			return f.Title + ": بله"
		}
		return f.Title + ": خیر"
	case string:
		return f.Title + ": " + label(x)
	case []string:
		parts := []string{}
		for _, s := range x {
			parts = append(parts, label(s))
		}
		return f.Title + ": " + strings.Join(parts, "، ")
	case divar.FilterNumberRange:
		parts := []string{}
		if x.Minimum != "" {
			parts = append(parts, "از "+x.Minimum)
		}
		if x.Maximum != "" {
			parts = append(parts, "تا "+x.Maximum)
		}
		return f.Title + ": " + strings.Join(parts, " ") + " " + f.Unit
	}
	return f.Title
}

func (s *Service) Summary(d *AlertDraft) string {
	if d.Plan == nil {
		return "چه آگهی‌ای می‌خواهی پیدا کنی؟ خواسته‌ات را با اسم شهر بنویس."
	}
	p := d.Plan
	var out strings.Builder
	fmt.Fprintf(&out, "این اعلان را آماده کردم:\n\n%s\n", p.Title)
	names := []string{}
	for _, id := range p.CityIDs {
		c := s.Catalogs.Cities[id]
		names = append(names, c.Name+" ("+c.Province+")")
	}
	fmt.Fprintf(&out, "شهر: %s\nدسته: %s\nبررسی: هر %d ثانیه\n", strings.Join(names, "، "), s.Catalogs.Categories[p.Category].Name, p.Interval)
	if len(d.Applied) > 0 {
		out.WriteString("\nشرایط جستجو:\n• " + strings.Join(d.Applied, "\n• ") + "\n")
	}
	if p.Keywords != "" {
		out.WriteString("عبارت جستجو: " + p.Keywords + "\n")
	}
	if len(p.AdditionalPreferences) > 0 {
		out.WriteString("\nبرای امتیاز و توضیح آگهی‌ها:\n• " + strings.Join(p.AdditionalPreferences, "\n• ") + "\nهمهٔ نتیجه‌های جستجو ارسال می‌شوند؛ امتیاز باعث حذف آگهی نمی‌شود.\n")
	}
	if len(p.Gaps) > 0 {
		out.WriteString("\nمواردی که باید بدانی:\n• " + strings.Join(p.Gaps, "\n• ") + "\n")
	}
	out.WriteString("\nبا تأیید، حداکثر سه آگهی فعلی را می‌فرستم و بعد آگهی‌های تازه را خبر می‌دهم.")
	return out.String()
}
