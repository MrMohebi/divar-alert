package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
)

type WatchState struct {
	Boundary        time.Time       `json:"boundary"`
	Watermark       time.Time       `json:"watermark"`
	Initialized     bool            `json:"initialized"`
	PreviewPrepared bool            `json:"preview_prepared"`
	Preview         []divar.Listing `json:"preview,omitempty"`
	Cursor          json.RawMessage `json:"cursor,omitempty"`
	ScanUpper       time.Time       `json:"scan_upper"`
}

type structuredWatcher struct {
	Search func(context.Context, divar.SearchQuery) (divar.SearchPage, error)
	Notify func(context.Context, db.Alert, divar.Listing) error
	Seen   func(string, int64) (bool, error)
	Record func(string, int64, any, int64) error
	Save   func(int64, string, int64) error
}

var watchSlots = make(chan struct{}, 4)
var watchLocks sync.Map
var watchJobs sync.WaitGroup

// Slow commentary for one alert must not block the scheduler for other chats.
func launchStructuredPoll(ctx context.Context, alert db.Alert) {
	if ctx.Err() != nil {
		return
	}
	value, _ := watchLocks.LoadOrStore(alert.Id, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	if !lock.TryLock() {
		return
	}
	select {
	case watchSlots <- struct{}{}:
	default:
		lock.Unlock()
		return
	}
	watchJobs.Add(1)
	go func() {
		defer watchJobs.Done()
		defer lock.Unlock()
		defer func() { <-watchSlots }()
		defer func() {
			if recovered := recover(); recovered != nil {
				sugar.Errorw("Structured poll panicked", "alert_id", alert.Id, "panic", recovered)
			}
		}()
		if err := pollStructuredAlert(ctx, alert); err != nil {
			sugar.Warnw("Structured alert poll failed", "alert_id", alert.Id, "error", err)
		}
	}()
}

func pollStructuredAlert(ctx context.Context, alert db.Alert) error {
	return (structuredWatcher{divar.SearchWithQuery, notifyStructuredListing, database.PostExists, database.InsertPost, database.UpdateWatchState}).Poll(ctx, alert)
}

func (w structuredWatcher) Poll(ctx context.Context, alert db.Alert) (pollErr error) {
	var query divar.SearchQuery
	var state WatchState
	if err := json.Unmarshal([]byte(alert.SearchQuery), &query); err != nil {
		return err
	}
	if alert.WatchState != "" {
		if err := json.Unmarshal([]byte(alert.WatchState), &state); err != nil {
			return err
		}
	}
	started := time.Now().UTC()
	if state.Boundary.IsZero() {
		state.Boundary = started
	}
	save := func() error {
		data, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return w.Save(alert.Id, string(data), started.Unix())
	}
	// On any failure retain the last committed cursor/watermark and retry next minute.
	defer func() {
		if err := save(); err != nil && pollErr == nil {
			pollErr = err
		}
	}()
	deliver := func(post divar.Listing) error {
		seen, err := w.Seen(post.Token, alert.Id)
		if err != nil {
			return err
		}
		if seen {
			return nil
		}
		if err = w.Notify(ctx, alert, post); err != nil {
			return err
		}
		return w.Record(post.Token, alert.Id, post, time.Now().Unix())
	}
	if !state.Initialized {
		if !state.PreviewPrepared {
			query.PaginationData = nil
			page, err := w.Search(ctx, query)
			if err != nil {
				return err
			}
			state.Preview = page.Listings
			if len(state.Preview) > 3 {
				state.Preview = state.Preview[:3]
			}
			state.PreviewPrepared = true
			if err = save(); err != nil {
				return err
			}
		}
		for _, post := range state.Preview {
			if err := deliver(post); err != nil {
				return err
			}
		}
		state.Initialized = true
		state.Preview = nil
		state.Watermark = state.Boundary
		return save()
	}
	for pageNumber := 0; pageNumber < 10; pageNumber++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		query.PaginationData = state.Cursor
		page, err := w.Search(ctx, query)
		if err != nil {
			return err
		}
		reachedBoundary := false
		for _, post := range page.Listings {
			if post.SortDate.IsZero() {
				return fmt.Errorf("listing %s lacks date; refusing to skip scan boundary", post.Token)
			}
			if post.SortDate.After(state.ScanUpper) {
				state.ScanUpper = post.SortDate
			}
			// Equality is included: multiple posts can share one timestamp.
			if post.SortDate.Before(state.Watermark) {
				reachedBoundary = true
				continue
			}
			if err = deliver(post); err != nil {
				return err
			}
		}
		if reachedBoundary || !page.HasNextPage {
			if state.ScanUpper.After(state.Watermark) {
				state.Watermark = state.ScanUpper
			}
			state.Cursor = nil
			state.ScanUpper = time.Time{}
			return save()
		}
		if len(page.Pagination) == 0 || string(page.Pagination) == "null" || string(page.Pagination) == string(state.Cursor) {
			return fmt.Errorf("search pagination did not advance")
		}
		state.Cursor = append(json.RawMessage(nil), page.Pagination...)
		if err = save(); err != nil {
			return err
		}
	}
	// Resume from the opaque cursor next cycle. Newer posts are picked up from
	// the head after this scan reaches its old watermark.
	return nil
}

// AI is strictly commentary. Errors, low scores, and unmet wishes never change
// whether this function attempts notification delivery.
type listingNotifier struct {
	GetPost       func(context.Context, string) (divar.PostDetailRes, error)
	Evaluate      func(context.Context, interface{}, []interface{}, string) (*ai.EvaluationResult, error)
	History       func(int64) ([]interface{}, error)
	AppendHistory func(int64, interface{}) error
	Send          func(context.Context, int64, string, string) error
}

func notifyStructuredListing(ctx context.Context, alert db.Alert, post divar.Listing) error {
	return (listingNotifier{divar.GetPostContext, ai.EvaluatePost, database.LoadHistory, database.AppendHistory, sendPostNotification}).Notify(ctx, alert, post)
}

func (n listingNotifier) Notify(ctx context.Context, alert db.Alert, post divar.Listing) error {
	var evaluation *ai.EvaluationResult
	var details divar.PostImportantData
	if alert.AIEnabled {
		raw, err := n.GetPost(ctx, post.Token)
		if err == nil {
			details = divar.ExtractImportantData(raw)
			history, _ := n.History(alert.Id)
			evaluation, err = n.Evaluate(ctx, details, history, alert.AnalysisPrompt)
		}
		if err != nil {
			evaluation = nil
			if sugar != nil {
				sugar.Warnw("Listing commentary unavailable", "alert_id", alert.Id, "error", err)
			}
		}
	}
	if err := n.Send(ctx, alert.ChatId, post.ImageURL, listingNotification(alert, post, evaluation)); err != nil {
		return err
	}
	if alert.AIEnabled && details.Token != "" {
		if err := n.AppendHistory(alert.Id, details); err != nil && sugar != nil {
			sugar.Warnw("Unable to save commentary history", "error", err)
		}
	}
	return nil
}

func listingNotification(alert db.Alert, post divar.Listing, evaluation *ai.EvaluationResult) string {
	var text strings.Builder
	fmt.Fprintf(&text, "آگهی تازه برای «%s»\n\n%s\n", alert.Title, post.Title)
	for _, line := range []string{post.TopDescriptionText, post.MiddleDescriptionText, post.BottomDescriptionText} {
		if line != "" {
			text.WriteString(line + "\n")
		}
	}
	if evaluation != nil {
		fmt.Fprintf(&text, "\nتحلیل آگهی\nامتیاز: %d از ۱۰۰\nپیشنهاد: %s\nتوضیح: %s\n", evaluation.Score, evaluation.Decision, evaluation.Explanation)
	} else if alert.AIEnabled {
		text.WriteString("\nتحلیل فعلاً در دسترس نیست؛ این آگهی را بدون امتیاز می‌فرستم.\n")
	}
	fmt.Fprintf(&text, "\nhttps://divar.ir/v/%s", post.Token)
	return text.String()
}
