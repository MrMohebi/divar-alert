package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"github.com/mrmohebi/divar-alert/platform"
)

func postWithLocation(t *testing.T, location string) divar.PostDetailRes {
	t.Helper()
	var post divar.PostDetailRes
	raw := `{"webengage":{"token":"abc"},"sections":[{"section_name":"MAP","widgets":[{"widget_type":"MAP_ROW","data":{"location":` + location + `}}]}]}`
	if err := json.Unmarshal([]byte(raw), &post); err != nil {
		t.Fatal(err)
	}
	return post
}

func TestListingTextKeepsListingWhateverTheCommentary(t *testing.T) {
	alert := db.Alert{Title: "خودرو", AIEnabled: true}
	post := divar.Listing{Token: "abc", Title: "خودرو"}
	for _, evaluation := range []*ai.EvaluationResult{
		{Score: 1, Decision: "پیشنهاد نمی‌شود", Explanation: "شرایط موردنظر را ندارد."},
		nil,
	} {
		text := listingText(alert, post, evaluation)
		if !strings.Contains(text, "https://divar.ir/v/abc") || !strings.Contains(text, "خودرو") {
			t.Fatal("commentary removed the listing")
		}
		if evaluation == nil && !strings.Contains(text, "بدون امتیاز") {
			t.Fatal("missing the Persian note about unavailable commentary")
		}
	}
}

func TestNotifierSendsDespiteLowScoresAndFailures(t *testing.T) {
	for _, mode := range []string{"low-score", "AI-error", "details-error"} {
		t.Run(mode, func(t *testing.T) {
			sends := 0
			n := notifier{
				GetPost: func(context.Context, string) (divar.PostDetailRes, error) {
					if mode == "details-error" {
						return divar.PostDetailRes{}, errors.New("offline")
					}
					return divar.PostDetailRes{Webengage: divar.Webengage{Token: "abc"}}, nil
				},
				Evaluate: func(context.Context, any, []any, string) (*ai.EvaluationResult, error) {
					if mode == "AI-error" {
						return nil, errors.New("offline")
					}
					return &ai.EvaluationResult{Score: 1, Decision: "پیشنهاد نمی‌شود", Explanation: "با خواسته هماهنگ نیست."}, nil
				},
				History:       func(int64) ([]any, error) { return nil, nil },
				AppendHistory: func(int64, any) error { return nil },
				Send: func(_ context.Context, _ platform.Chat, _, text string) (int, error) {
					sends++
					if !strings.Contains(text, "https://divar.ir/v/abc") {
						t.Fatal("missing post link")
					}
					return 1, nil
				},
			}
			if err := n.Notify(context.Background(), db.Alert{AIEnabled: true}, divar.Listing{Token: "abc", Title: "خودرو"}); err != nil || sends != 1 {
				t.Fatalf("sends=%d err=%v", sends, err)
			}
		})
	}
}

func TestNotifierPinsLocationAfterListing(t *testing.T) {
	tests := []struct {
		name        string
		post        divar.PostDetailRes
		locationErr error
		wantPin     bool
	}{
		{"approximate", postWithLocation(t, `{"type":"FUZZY","fuzzy_data":{"point":{"latitude":30.29,"longitude":57.04},"radius":500}}`), nil, true},
		{"no location", divar.PostDetailRes{}, nil, false},
		{"pin fails", postWithLocation(t, `{"type":"EXACT","exact_data":{"point":{"latitude":30.26,"longitude":57.03}}}`), errors.New("offline"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var events []string
			var pinned divar.LocationInfo
			n := notifier{
				GetPost: func(context.Context, string) (divar.PostDetailRes, error) { return tt.post, nil },
				Send: func(_ context.Context, chat platform.Chat, _, _ string) (int, error) {
					events = append(events, "listing:"+chat.String())
					return 77, nil
				},
				SendLocation: func(_ context.Context, chat platform.Chat, replyTo int, location divar.LocationInfo) error {
					events = append(events, fmt.Sprintf("pin:%s:%d", chat, replyTo))
					pinned = location
					return tt.locationErr
				},
			}
			// AI is off, so details are fetched for the location alone.
			if err := n.Notify(context.Background(), db.Alert{Chat: baleChat}, divar.Listing{Token: "abc"}); err != nil {
				t.Fatalf("Notify() error = %v", err)
			}
			want := []string{"listing:bale:42"}
			if tt.wantPin {
				want = append(want, "pin:bale:42:77")
			}
			if strings.Join(events, ",") != strings.Join(want, ",") {
				t.Fatalf("events = %v, want %v", events, want)
			}
			if tt.name == "approximate" && pinned.RadiusMeters != 500 {
				t.Fatalf("pinned = %+v", pinned)
			}
		})
	}
}

func TestAnalysisGoalIncludesRequestAndPreferences(t *testing.T) {
	goal := analysisGoal(db.Alert{OriginalPrompt: "آپارتمان در کرمان", AnalysisPrompt: "پارکینگ داشته باشد"})
	for _, part := range []string{"آپارتمان در کرمان", "پارکینگ داشته باشد", "Persian"} {
		if !strings.Contains(goal, part) {
			t.Fatalf("goal %q lacks %q", goal, part)
		}
	}
}

// A listing, its AI commentary and its map pin all reach the alert's own
// platform, and nothing reaches the other one.
func TestDeliveryUsesTheAlertsPlatform(t *testing.T) {
	ta := newTestApp(t)
	ta.getPost = func(context.Context, string) (divar.PostDetailRes, error) {
		return postWithLocation(t, `{"type":"EXACT","exact_data":{"point":{"latitude":30.26,"longitude":57.03}}}`), nil
	}
	ta.evaluate = func(context.Context, any, []any, string) (*ai.EvaluationResult, error) {
		return &ai.EvaluationResult{Score: 80, Decision: "مناسب", Explanation: "قیمت خوبی دارد."}, nil
	}
	alert := db.Alert{ID: 1, Chat: baleChat, Title: "خانه", AIEnabled: true}
	if err := ta.notifier().Notify(context.Background(), alert, divar.Listing{Token: "abc", Title: "خانه"}); err != nil {
		t.Fatal(err)
	}
	if texts := ta.bale.texts("sendMessage"); len(texts) != 1 || !strings.Contains(texts[0], "امتیاز: 80") {
		t.Fatalf("bale messages = %q", texts)
	}
	if len(ta.bale.texts("sendLocation")) != 1 || len(ta.telegram.calls) != 0 {
		t.Fatalf("bale calls=%+v telegram calls=%+v", ta.bale.calls, ta.telegram.calls)
	}

	delete(ta.bots, platform.Bale)
	if _, err := ta.sendListing(context.Background(), baleChat, "", "آگهی"); err == nil {
		t.Fatal("sent through a platform without a bot")
	}
}

func TestDelayedScanCannotDeliverAfterDeleteOrReset(t *testing.T) {
	for _, reset := range []bool{false, true} {
		name := "delete"
		if reset {
			name = "reset"
		}
		t.Run(name, func(t *testing.T) {
			ta := newTestApp(t)
			alert := db.Alert{ID: 1, Chat: telegramChat, Title: "خودرو", Interval: 60,
				SearchQuery: `{"city_ids":["13"]}`, WatchState: `{"initialized":true,"watermark":"2026-01-01T00:00:00Z"}`}
			if err := ta.store.InsertAlert(alert); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			ta.search = func(context.Context, divar.SearchQuery) (divar.SearchPage, error) {
				close(entered)
				<-release
				return divar.SearchPage{Listings: []divar.Listing{listing("new", time.Now())}}, nil
			}
			ta.getPost = func(context.Context, string) (divar.PostDetailRes, error) { return divar.PostDetailRes{}, nil }
			done := make(chan error, 1)
			go func() { done <- ta.watcher(alert).Poll(context.Background(), alert) }()
			<-entered
			var err error
			if reset {
				err = ta.store.ResetAlert(telegramChat, 1)
			} else {
				err = ta.store.DeleteAlert(telegramChat, 1)
			}
			if err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-done; !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("stale scan error = %v", err)
			}
			if calls := ta.telegram.calls; len(calls) != 0 {
				t.Fatalf("late sends = %+v", calls)
			}
			if delivered, _ := ta.store.Delivered(1, "new"); delivered {
				t.Fatal("late delivery recorded")
			}
			if reset {
				alerts, _ := ta.store.ActiveAlerts()
				if len(alerts) != 1 || alerts[0].WatchState != "{}" || alerts[0].LastChecked != 0 {
					t.Fatalf("reset state overwritten: %+v", alerts)
				}
			}
		})
	}
}

func TestSplitTextKeepsEverythingWithinTheLimit(t *testing.T) {
	line := strings.Repeat("کلمه ", 30) // 150 runes
	text := strings.Repeat(line+"\n", 20) + "https://divar.ir/v/abc"
	for _, limit := range []int{100, 1024, 4096} {
		parts := splitText(text, limit)
		for _, part := range parts {
			if n := len([]rune(part)); n > limit || n == 0 {
				t.Fatalf("limit %d: part of %d runes", limit, n)
			}
		}
		if !strings.HasSuffix(parts[len(parts)-1], "https://divar.ir/v/abc") {
			t.Fatalf("limit %d: link lost or broken: %q", limit, parts[len(parts)-1])
		}
		if got, want := strings.Join(strings.Fields(strings.Join(parts, " ")), " "), strings.Join(strings.Fields(text), " "); got != want {
			t.Fatalf("limit %d: text changed", limit)
		}
	}
	if parts := splitText(strings.Repeat("a", 250), 100); len(parts) != 3 || parts[2] != strings.Repeat("a", 50) {
		t.Fatalf("unbreakable text parts = %q", parts)
	}
	if parts := splitText("short", 100); len(parts) != 1 || parts[0] != "short" {
		t.Fatalf("short text parts = %q", parts)
	}
}

// A listing too long for a photo caption continues in follow-up messages, so
// its link still arrives.
func TestLongListingContinuesAfterThePhoto(t *testing.T) {
	ta := newTestApp(t)
	text := strings.Repeat(strings.Repeat("توضیح ", 20)+"\n", 30) + "https://divar.ir/v/abc"
	id, err := ta.sendListing(context.Background(), telegramChat, "https://example.com/a.jpg", text)
	if err != nil || id != 1 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	photos, messages := ta.telegram.texts("sendPhoto"), ta.telegram.texts("sendMessage")
	if len(photos) != 1 || len([]rune(photos[0])) > maxCaptionLength || len(messages) == 0 {
		t.Fatalf("photos=%d messages=%d", len(photos), len(messages))
	}
	if !strings.HasSuffix(messages[len(messages)-1], "https://divar.ir/v/abc") {
		t.Fatal("listing link missing from the follow-up message")
	}
}
