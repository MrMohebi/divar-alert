package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mrmohebi/divar-alert/platform"
)

var (
	telegramChat = platform.Chat{Platform: platform.Telegram, ID: 42}
	baleChat     = platform.Chat{Platform: platform.Bale, ID: 42}
)

const testQuery = `{"city_ids":["13"]}`

func openTestDB(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bot.db")
	database, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database, path
}

func testAlert(id int64, chat platform.Chat) Alert {
	return Alert{ID: id, Chat: chat, Title: "خودرو", SearchQuery: testQuery, Interval: 60}
}

func TestSchemaIsVersionedAndOtherDatabasesAreRefused(t *testing.T) {
	database, path := openTestDB(t)
	var version int
	if err := database.conn.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != SchemaVersion {
		t.Fatalf("user_version=%d err=%v", version, err)
	}
	if err := database.InsertAlert(testAlert(1, telegramChat)); err != nil {
		t.Fatal(err)
	}
	database.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	alerts, _ := reopened.ActiveAlerts()
	reopened.Close()
	if len(alerts) != 1 {
		t.Fatalf("alerts after reopen = %+v", alerts)
	}

	// A database from an earlier version has tables but no schema version.
	oldPath := filepath.Join(t.TempDir(), "old.db")
	conn, err := sql.Open("sqlite", oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`CREATE TABLE alerts (id INTEGER PRIMARY KEY, chat_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if old, err := Open(oldPath); err == nil {
		old.Close()
		t.Fatal("opened a database from an earlier version")
	}
}

func TestSamePlatformChatIDsAreIsolated(t *testing.T) {
	database, _ := openTestDB(t)
	if err := database.InsertAlert(testAlert(1, telegramChat)); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertAlert(testAlert(2, baleChat)); err != nil {
		t.Fatal(err)
	}
	for _, chat := range []platform.Chat{telegramChat, baleChat} {
		alerts, err := database.ChatAlerts(chat)
		if err != nil || len(alerts) != 1 || alerts[0].Chat != chat {
			t.Fatalf("%s alerts=%+v err=%v", chat, alerts, err)
		}
	}
	for name, mutate := range map[string]func() error{
		"delete": func() error { return database.DeleteAlert(telegramChat, 2) },
		"reset":  func() error { return database.ResetAlert(telegramChat, 2) },
		"AI":     func() error { return database.SetAlertAI(telegramChat, 2, false) },
	} {
		if err := mutate(); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("%s: a Telegram chat changed a Bale alert: %v", name, err)
		}
	}
	other := platform.Chat{Platform: platform.Bale, ID: 99}
	if err := database.DeleteAlert(other, 2); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another chat deleted the alert: %v", err)
	}

	if err := database.SaveDraft(telegramChat, `{"from":"telegram"}`); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveDraft(baleChat, `{"from":"bale"}`); err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteDraft(telegramChat); err != nil {
		t.Fatal(err)
	}
	if draft, _ := database.Draft(baleChat); draft != `{"from":"bale"}` {
		t.Fatalf("bale draft = %q", draft)
	}
	if draft, _ := database.Draft(telegramChat); draft != "" {
		t.Fatalf("telegram draft = %q", draft)
	}
	if err := database.ConfirmDraft(telegramChat, `{"from":"bale"}`, testAlert(3, telegramChat)); !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("Telegram confirmed a Bale draft: %v", err)
	}
	if err := database.ConfirmDraft(telegramChat, `{"from":"bale"}`, testAlert(3, baleChat)); err == nil {
		t.Fatal("confirmed an alert for another chat")
	}
}

func TestInvalidDraftIsRejectedWithoutLosingTheSavedOne(t *testing.T) {
	database, _ := openTestDB(t)
	if err := database.SaveDraft(telegramChat, `{"step":1}`); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveDraft(telegramChat, `not-json`); err == nil {
		t.Fatal("saved invalid JSON")
	}
	if draft, _ := database.Draft(telegramChat); draft != `{"step":1}` {
		t.Fatalf("draft = %q", draft)
	}
}

func TestConfirmDraftIsAtomicAndIdempotent(t *testing.T) {
	database, _ := openTestDB(t)
	raw := `{"id":"draft","revision":2}`
	if err := database.SaveDraft(telegramChat, raw); err != nil {
		t.Fatal(err)
	}
	alert := testAlert(1, telegramChat)
	alert.OriginalPrompt = "خودرو در کرمان"
	if err := database.ConfirmDraft(telegramChat, `{"revision":1}`, alert); !errors.Is(err, ErrStaleDraft) {
		t.Fatal("an old revision confirmed the draft")
	}

	invalid := alert
	invalid.Interval = 1
	if err := database.ConfirmDraft(telegramChat, raw, invalid); err == nil {
		t.Fatal("inserted an invalid alert")
	}
	if draft, _ := database.Draft(telegramChat); draft != raw {
		t.Fatal("a failed insert consumed the draft")
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- database.ConfirmDraft(telegramChat, raw, alert) }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrStaleDraft) {
			t.Fatal(err)
		}
	}
	alerts, _ := database.ActiveAlerts()
	draft, _ := database.Draft(telegramChat)
	if successes != 1 || len(alerts) != 1 || draft != "" || alerts[0].OriginalPrompt != alert.OriginalPrompt {
		t.Fatalf("confirmation not atomic: successes=%d alerts=%+v draft=%q", successes, alerts, draft)
	}
}

func TestDeletedAlertIsRetainedButInactive(t *testing.T) {
	database, _ := openTestDB(t)
	alert := testAlert(1, telegramChat)
	if err := database.InsertAlert(alert); err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteAlert(telegramChat, 1); err != nil {
		t.Fatal(err)
	}
	var deleted int64
	if err := database.conn.QueryRow(`SELECT deleted_at FROM alerts WHERE id = 1`).Scan(&deleted); err != nil || deleted == 0 {
		t.Fatalf("deleted_at=%d err=%v", deleted, err)
	}
	if all, _ := database.ActiveAlerts(); len(all) != 0 {
		t.Fatalf("deleted alert still active: %+v", all)
	}
	for name, mutate := range map[string]func() error{
		"delete":     func() error { return database.DeleteAlert(telegramChat, 1) },
		"reset":      func() error { return database.ResetAlert(telegramChat, 1) },
		"AI":         func() error { return database.SetAlertAI(telegramChat, 1, true) },
		"checkpoint": func() error { return database.SaveWatchState(1, 0, `{}`, 99) },
		"deliver": func() error {
			return database.WithActiveAlert(alert, func() error { t.Error("deleted alert delivered"); return nil })
		},
	} {
		if err := mutate(); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("%s on deleted alert: %v", name, err)
		}
	}
}

func TestResetRetiresDeliveriesAndInvalidatesOldWork(t *testing.T) {
	database, _ := openTestDB(t)
	alert := testAlert(1, telegramChat)
	if err := database.InsertAlert(alert); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordDelivery(1, "a", map[string]string{"title": "first"}, 123); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordDelivery(1, "a", nil, 124); err == nil {
		t.Fatal("recorded the same active delivery twice")
	}
	if err := database.AppendHistory(1, "first"); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveWatchState(1, 0, `{"initialized":true}`, 10); err != nil {
		t.Fatal(err)
	}
	if err := database.ResetAlert(telegramChat, 1); err != nil {
		t.Fatal(err)
	}
	if err := database.WithActiveAlert(alert, func() error { t.Error("old generation delivered"); return nil }); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("old generation: %v", err)
	}
	if err := database.SaveWatchState(1, 0, `{"initialized":true}`, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("old generation saved a checkpoint: %v", err)
	}
	current, _ := database.ActiveAlerts()
	if len(current) != 1 || current[0].Generation != 1 || current[0].WatchState != "{}" || current[0].LastChecked != 0 {
		t.Fatalf("reset alert = %+v", current)
	}
	if delivered, _ := database.Delivered(1, "a"); delivered {
		t.Fatal("reset kept the delivery active")
	}
	if history, _ := database.LoadHistory(1); len(history) != 0 {
		t.Fatalf("reset kept AI history: %v", history)
	}
	if err := database.WithActiveAlert(current[0], func() error { return database.RecordDelivery(1, "a", nil, 456) }); err != nil {
		t.Fatal(err)
	}
	var total, retired int
	if err := database.conn.QueryRow(`SELECT COUNT(*), COUNT(retired_at) FROM deliveries`).Scan(&total, &retired); err != nil || total != 2 || retired != 1 {
		t.Fatalf("deliveries total=%d retired=%d err=%v", total, retired, err)
	}
}

func TestDeleteAndResetWaitForDelivery(t *testing.T) {
	for _, reset := range []bool{false, true} {
		name := "delete"
		if reset {
			name = "reset"
		}
		t.Run(name, func(t *testing.T) {
			database, _ := openTestDB(t)
			alert := testAlert(1, telegramChat)
			if err := database.InsertAlert(alert); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			delivered := make(chan error, 1)
			go func() {
				delivered <- database.WithActiveAlert(alert, func() error {
					close(entered)
					<-release
					return database.RecordDelivery(1, "a", nil, 123)
				})
			}()
			<-entered
			finished := make(chan error, 1)
			go func() {
				if reset {
					finished <- database.ResetAlert(telegramChat, 1)
				} else {
					finished <- database.DeleteAlert(telegramChat, 1)
				}
			}()
			select {
			case err := <-finished:
				t.Fatalf("%s passed an active delivery: %v", name, err)
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			if err := <-delivered; err != nil {
				t.Fatal(err)
			}
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			if err := database.WithActiveAlert(alert, func() error { t.Error("delivered after the change returned"); return nil }); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal(err)
			}
			var count int
			if err := database.conn.QueryRow(`SELECT COUNT(*) FROM deliveries`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("delivery lost: %d %v", count, err)
			}
		})
	}
}

func TestHistoryIsBoundedAndSurvivesReopen(t *testing.T) {
	database, path := openTestDB(t)
	for _, id := range []int64{1, 2} {
		if err := database.InsertAlert(testAlert(id, telegramChat)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 65; i++ {
		if err := database.AppendHistory(1, i); err != nil {
			t.Fatal(err)
		}
		if i < 7 {
			if err := database.AppendHistory(2, i); err != nil {
				t.Fatal(err)
			}
		}
	}
	database.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for _, check := range []struct {
		id           int64
		count, first int
	}{{1, maxHistoryPosts, 65 - maxHistoryPosts}, {2, 7, 0}} {
		history, err := reopened.LoadHistory(check.id)
		if err != nil || len(history) != check.count {
			t.Fatalf("alert %d: loaded=%d err=%v; want %d", check.id, len(history), err, check.count)
		}
		for i, post := range history {
			if post != float64(check.first+i) {
				t.Fatalf("alert %d history[%d] = %v; want %d", check.id, i, post, check.first+i)
			}
		}
	}
}

func TestWatchStateSurvivesReopen(t *testing.T) {
	database, path := openTestDB(t)
	if err := database.InsertAlert(testAlert(1, baleChat)); err != nil {
		t.Fatal(err)
	}
	state := `{"initialized":true,"cursor":{"opaque":42}}`
	if err := database.SaveWatchState(1, 0, state, 10); err != nil {
		t.Fatal(err)
	}
	database.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	alerts, _ := reopened.ChatAlerts(baleChat)
	if len(alerts) != 1 || alerts[0].WatchState != state || alerts[0].LastChecked != 10 || alerts[0].Chat != baleChat {
		t.Fatalf("alerts after reopen = %+v", alerts)
	}
}
