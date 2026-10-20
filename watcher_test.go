package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
)

type watchFixture struct {
	alert   db.Alert
	state   WatchState
	seen    map[string]bool
	sent    []string
	fail    string
	watcher structuredWatcher
}

func newWatchFixture(t *testing.T) *watchFixture {
	t.Helper()
	f := &watchFixture{alert: db.Alert{Id: 1, ChatId: 42, Interval: 60, SearchQuery: `{"city_ids":["13"],"search_data":{"form_data":{"data":{"category":{"str":{"value":"light"}}}}}}`}, seen: map[string]bool{}}
	f.watcher = structuredWatcher{
		Seen: func(token string, _ int64) (bool, error) { return f.seen[token], nil },
		Notify: func(ctx context.Context, a db.Alert, p divar.Listing) error {
			if p.Token == f.fail {
				return errors.New("delivery failed")
			}
			f.sent = append(f.sent, p.Token)
			return nil
		},
		Record: func(token string, _ int64, _ any, _ int64) error { f.seen[token] = true; return nil },
		Save: func(_ int64, s string, _ int64) error {
			f.alert.WatchState = s
			f.state = WatchState{}
			return json.Unmarshal([]byte(s), &f.state)
		},
	}
	return f
}
func listing(token string, stamp time.Time) divar.Listing {
	return divar.Listing{Token: token, Title: "آگهی", SortDate: stamp}
}

func TestThreeInitialResultsThenOnlyNewPosts(t *testing.T) {
	f := newWatchFixture(t)
	now := time.Now().UTC()
	boundary := now.Add(-time.Minute)
	state, _ := json.Marshal(WatchState{Boundary: boundary})
	f.alert.WatchState = string(state)
	f.watcher.Search = func(context.Context, divar.SearchQuery) (divar.SearchPage, error) {
		return divar.SearchPage{Listings: []divar.Listing{listing("a", boundary.Add(-time.Second)), listing("b", boundary.Add(-2*time.Second)), listing("c", boundary.Add(-3*time.Second)), listing("old", boundary.Add(-4*time.Second))}}, nil
	}
	if err := f.watcher.Poll(context.Background(), f.alert); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.sent, []string{"a", "b", "c"}) || !f.state.Initialized {
		t.Fatalf("wrong preview %+v", f)
	}
	f.watcher.Search = func(context.Context, divar.SearchQuery) (divar.SearchPage, error) {
		return divar.SearchPage{Listings: []divar.Listing{listing("new", now), listing("old", boundary.Add(-time.Second))}}, nil
	}
	if err := f.watcher.Poll(context.Background(), f.alert); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.sent, []string{"a", "b", "c", "new"}) {
		t.Fatalf("old results flooded: %v", f.sent)
	}
	if err := f.watcher.Poll(context.Background(), f.alert); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 4 {
		t.Fatal("duplicate notification")
	}
}

func TestPaginationDeliveryFailureRetriesWithoutAdvancingWatermark(t *testing.T) {
	f := newWatchFixture(t)
	now := time.Now().UTC()
	boundary := now.Add(-time.Hour)
	state, _ := json.Marshal(WatchState{Boundary: boundary, Watermark: boundary, Initialized: true})
	f.alert.WatchState = string(state)
	cursor := json.RawMessage(`{"page":2,"opaque":{"viewed_tokens":"keep"}}`)
	f.watcher.Search = func(ctx context.Context, q divar.SearchQuery) (divar.SearchPage, error) {
		if len(q.PaginationData) == 0 {
			return divar.SearchPage{Listings: []divar.Listing{listing("new1", now)}, HasNextPage: true, Pagination: cursor}, nil
		}
		if string(q.PaginationData) != string(cursor) {
			t.Fatalf("lost cursor %s", q.PaginationData)
		}
		return divar.SearchPage{Listings: []divar.Listing{listing("new2", now.Add(-time.Minute)), listing("old", boundary.Add(-time.Second))}}, nil
	}
	f.fail = "new2"
	if err := f.watcher.Poll(context.Background(), f.alert); err == nil {
		t.Fatal("delivery failure hidden")
	}
	if !f.state.Watermark.Equal(boundary) || !f.seen["new1"] || f.seen["new2"] {
		t.Fatal("failure advanced state or recorded failed delivery")
	}
	f.fail = ""
	if err := f.watcher.Poll(context.Background(), f.alert); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.sent, []string{"new1", "new2"}) || !f.state.Watermark.Equal(now) || len(f.state.Cursor) != 0 {
		t.Fatalf("retry state wrong: %+v", f)
	}
}

func TestPreviewFailureSurvivesRestartWithoutChangingSnapshot(t *testing.T) {
	f := newWatchFixture(t)
	n := time.Now().UTC()
	calls := 0
	f.watcher.Search = func(context.Context, divar.SearchQuery) (divar.SearchPage, error) {
		calls++
		return divar.SearchPage{Listings: []divar.Listing{listing("a", n), listing("b", n)}}, nil
	}
	f.fail = "b"
	if err := f.watcher.Poll(context.Background(), f.alert); err == nil {
		t.Fatal("expected failed send")
	}
	if f.state.Initialized || !f.state.PreviewPrepared {
		t.Fatal("initialization lost snapshot")
	}
	f.fail = ""
	if err := f.watcher.Poll(context.Background(), f.alert); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !reflect.DeepEqual(f.sent, []string{"a", "b"}) {
		t.Fatal("restart duplicated or replaced preview")
	}
}

func TestLongScanCheckpointsAndResumes(t *testing.T) {
	f := newWatchFixture(t)
	n := time.Now().UTC()
	old := n.Add(-time.Hour)
	state, _ := json.Marshal(WatchState{Boundary: old, Watermark: old, Initialized: true})
	f.alert.WatchState = string(state)
	f.watcher.Search = func(ctx context.Context, q divar.SearchQuery) (divar.SearchPage, error) {
		page := 0
		var cursor struct {
			Page int `json:"page"`
		}
		if len(q.PaginationData) > 0 {
			_ = json.Unmarshal(q.PaginationData, &cursor)
			page = cursor.Page
		}
		next, _ := json.Marshal(map[string]int{"page": page + 1})
		return divar.SearchPage{Listings: []divar.Listing{listing(fmt.Sprint(page), n.Add(-time.Duration(page)*time.Second))}, HasNextPage: page < 10, Pagination: next}, nil
	}
	if err := f.watcher.Poll(context.Background(), f.alert); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 10 || len(f.state.Cursor) == 0 || !f.state.Watermark.Equal(old) {
		t.Fatal("scan did not checkpoint")
	}
	if err := f.watcher.Poll(context.Background(), f.alert); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 11 || len(f.state.Cursor) != 0 {
		t.Fatal("scan did not resume")
	}
}

func TestCommentaryScoresAndFailuresNeverRemoveNotification(t *testing.T) {
	alert := db.Alert{Title: "خودرو", AIEnabled: true}
	post := divar.Listing{Token: "abc", Title: "خودرو"}
	for _, evaluation := range []*ai.EvaluationResult{
		{Score: 1, Decision: "پیشنهاد نمی‌شود", Explanation: "شرایط موردنظر را ندارد."},
		{Score: 45, Decision: "اطلاعات ناکافی", Explanation: "مناسب بودن برای خانواده روشن نیست."},
		nil,
	} {
		text := listingNotification(alert, post, evaluation)
		if !strings.Contains(text, "https://divar.ir/v/abc") || !strings.Contains(text, "خودرو") {
			t.Fatal("commentary removed listing")
		}
		if evaluation == nil && !strings.Contains(text, "بدون امتیاز") {
			t.Fatal("failure missing Persian explanation")
		}
	}
}

func TestNotifierActuallySendsLowScoresAndDependencyFailures(t *testing.T) {
	for _, mode := range []string{"low-score", "uncertain", "AI-error", "details-error"} {
		t.Run(mode, func(t *testing.T) {
			sends := 0
			notifier := listingNotifier{
				GetPost: func(context.Context, string) (divar.PostDetailRes, error) {
					if mode == "details-error" {
						return divar.PostDetailRes{}, errors.New("offline")
					}
					return divar.PostDetailRes{Webengage: divar.Webengage{Token: "abc"}}, nil
				},
				Evaluate: func(context.Context, interface{}, []interface{}, string) (*ai.EvaluationResult, error) {
					if mode == "AI-error" {
						return nil, errors.New("offline")
					}
					return &ai.EvaluationResult{Score: 1, Decision: "پیشنهاد نمی‌شود", Explanation: "شرایط قابل تأیید نیست یا با خواسته هماهنگ نیست."}, nil
				},
				History:       func(int64) ([]interface{}, error) { return nil, nil },
				AppendHistory: func(int64, interface{}) error { return nil },
				Send: func(ctx context.Context, id int64, image, text string) error {
					sends++
					if !strings.Contains(text, "https://divar.ir/v/abc") {
						t.Fatal("missing post link")
					}
					return nil
				},
			}
			if err := notifier.Notify(context.Background(), db.Alert{AIEnabled: true, AnalysisPrompt: "برای خانواده مناسب باشد"}, divar.Listing{Token: "abc", Title: "خودرو"}); err != nil || sends != 1 {
				t.Fatalf("AI gated notification: sends=%d err=%v", sends, err)
			}
		})
	}
}
