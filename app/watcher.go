package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"github.com/mrmohebi/divar-alert/platform"
)

// previewSize is how many current listings a new alert sends at first.
const previewSize = 3

// maxPagesPerScan bounds one scan; a longer backlog resumes next time.
const maxPagesPerScan = 10

// WatchState is an alert's scan checkpoint.
type WatchState struct {
	// Boundary is when the alert started; older listings are only previewed.
	Boundary time.Time `json:"boundary"`
	// Watermark is the newest listing date already scanned.
	Watermark       time.Time       `json:"watermark"`
	Initialized     bool            `json:"initialized"`
	PreviewPrepared bool            `json:"preview_prepared"`
	Preview         []divar.Listing `json:"preview,omitempty"`
	// Cursor resumes a multi-page scan; it is opaque to us.
	Cursor    json.RawMessage `json:"cursor,omitempty"`
	ScanUpper time.Time       `json:"scan_upper"`
}

// runScheduler starts a scan for each alert that is due, until ctx ends.
func (a *App) runScheduler(ctx context.Context) {
	for ctx.Err() == nil {
		a.scheduleDueAlerts(ctx)
		if !sleep(ctx, a.pollInterval) {
			return
		}
	}
}

func (a *App) scheduleDueAlerts(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			a.log.Errorw("Scheduler panicked", "panic", recovered)
		}
	}()
	alerts, err := a.store.ActiveAlerts()
	if err != nil {
		a.log.Errorw("Failed to read alerts", "error", err)
		return
	}
	now := time.Now().Unix()
	for _, alert := range alerts {
		if alert.LastChecked+int64(alert.Interval) > now {
			continue
		}
		if _, ok := a.bots[alert.Chat.Platform]; !ok {
			a.warnUnserved(alert.Chat.Platform)
			continue
		}
		a.launchPoll(ctx, alert)
	}
}

// warnUnserved logs once per platform whose alerts wait for a bot token.
func (a *App) warnUnserved(p platform.Platform) {
	if _, warned := a.unservedLog.LoadOrStore(p, struct{}{}); !warned {
		a.log.Warnw("Alerts paused: no bot configured for their platform", "platform", p)
	}
}

// launchPoll scans an alert in the background, so slow AI commentary for one
// alert does not hold up the others. An alert already being scanned, or a
// full worker pool, is skipped until the next pass.
func (a *App) launchPoll(ctx context.Context, alert db.Alert) {
	if ctx.Err() != nil {
		return
	}
	value, _ := a.pollLocks.LoadOrStore(alert.ID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	if !lock.TryLock() {
		return
	}
	select {
	case a.pollSlots <- struct{}{}:
	default:
		lock.Unlock()
		return
	}
	a.pollJobs.Add(1)
	go func() {
		defer a.pollJobs.Done()
		defer lock.Unlock()
		defer func() { <-a.pollSlots }()
		defer func() {
			if recovered := recover(); recovered != nil {
				a.log.Errorw("Alert scan panicked", "alert_id", alert.ID, "panic", recovered)
			}
		}()
		if err := a.watcher(alert).Poll(ctx, alert); err != nil {
			a.log.Warnw("Alert scan failed", "alert_id", alert.ID, "chat", alert.Chat, "error", err)
		}
	}()
}

func (a *App) watcher(alert db.Alert) watcher {
	return watcher{
		Search:    a.search,
		Notify:    a.notifier().Notify,
		Delivered: a.store.Delivered,
		Record:    a.store.RecordDelivery,
		Guard:     a.store.WithActiveAlert,
		Save: func(id int64, state string, checked int64) error {
			return a.store.SaveWatchState(id, alert.Generation, state, checked)
		},
	}
}

// watcher scans one alert's search and delivers each new listing once.
type watcher struct {
	Search    func(context.Context, divar.SearchQuery) (divar.SearchPage, error)
	Notify    func(context.Context, db.Alert, divar.Listing) error
	Delivered func(alertID int64, token string) (bool, error)
	Record    func(alertID int64, token string, data any, at int64) error
	Save      func(alertID int64, state string, checked int64) error
	// Guard runs a delivery only while the alert is still current.
	Guard func(db.Alert, func() error) error
}

// Poll runs one scan. A new alert first sends a preview of current listings;
// later scans walk newest-first until they reach the watermark. On failure
// the last committed checkpoint is kept and the scan is retried next time.
func (w watcher) Poll(ctx context.Context, alert db.Alert) (pollErr error) {
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
		return w.Save(alert.ID, string(data), started.Unix())
	}
	defer func() {
		if err := save(); err != nil && pollErr == nil {
			pollErr = err
		}
	}()
	deliver := func(post divar.Listing) error {
		commit := func() error {
			delivered, err := w.Delivered(alert.ID, post.Token)
			if err != nil || delivered {
				return err
			}
			if err := w.Notify(ctx, alert, post); err != nil {
				return err
			}
			return w.Record(alert.ID, post.Token, post, time.Now().Unix())
		}
		if w.Guard != nil {
			return w.Guard(alert, commit)
		}
		return commit()
	}

	if !state.Initialized {
		if !state.PreviewPrepared {
			query.PaginationData = nil
			page, err := w.Search(ctx, query)
			if err != nil {
				return err
			}
			state.Preview = page.Listings
			if len(state.Preview) > previewSize {
				state.Preview = state.Preview[:previewSize]
			}
			state.PreviewPrepared = true
			if err := save(); err != nil {
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

	for range maxPagesPerScan {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		query.PaginationData = state.Cursor
		page, err := w.Search(ctx, query)
		if err != nil {
			return err
		}
		reachedWatermark := false
		for _, post := range page.Listings {
			if post.SortDate.IsZero() {
				return fmt.Errorf("listing %s lacks a date; refusing to skip past the watermark", post.Token)
			}
			if post.SortDate.After(state.ScanUpper) {
				state.ScanUpper = post.SortDate
			}
			// Equal dates are delivered: several listings can share one.
			if post.SortDate.Before(state.Watermark) {
				reachedWatermark = true
				continue
			}
			if err := deliver(post); err != nil {
				return err
			}
		}
		if reachedWatermark || !page.HasNextPage {
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
		if err := save(); err != nil {
			return err
		}
	}
	// The backlog continues from the cursor next time; newer listings are
	// picked up from the top once this scan reaches the watermark.
	return nil
}
