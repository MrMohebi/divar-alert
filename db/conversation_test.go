package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestMigrateLegacyDatabasePreservesAlerts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(`CREATE TABLE alerts(id INTEGER PRIMARY KEY,chat_id INTEGER NOT NULL,title TEXT NOT NULL,link TEXT NOT NULL,ai_enabled BOOLEAN NOT NULL DEFAULT true,analysis_prompt TEXT NOT NULL DEFAULT '',interval_seconds INTEGER NOT NULL,last_time_checked INTEGER NOT NULL);
 INSERT INTO alerts VALUES(1,42,'قدیمی','curl',true,'بررسی قیمت',300,123);`)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	alerts, err := database.GetAllAlerts()
	if err != nil || len(alerts) != 1 {
		t.Fatalf("alerts=%+v err=%v", alerts, err)
	}
	if alerts[0].Link != "curl" || alerts[0].Interval != 300 || alerts[0].AnalysisPrompt != "بررسی قیمت" || alerts[0].SearchQuery != "" {
		t.Fatal("migration changed legacy configuration")
	}
	database.Close()
	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
}

func TestConfirmDraftAtomicOwnershipAndIdempotency(t *testing.T) {
	database := openTestDB(t, "confirm")
	raw := `{"id":"draft","revision":2}`
	if err := database.UpsertProcess(42, "NATURAL_ALERT", raw, 1); err != nil {
		t.Fatal(err)
	}
	alert := Alert{Id: 1, ChatId: 42, Title: "خودرو", Interval: 60, SearchQuery: `{"city_ids":["13"]}`, OriginalPrompt: "خودرو در کرمان"}
	if err := database.ConfirmDraft(99, "NATURAL_ALERT", raw, alert); err == nil {
		t.Fatal("wrong owner confirmed draft")
	}
	if err := database.ConfirmDraft(42, "NATURAL_ALERT", `{"revision":1}`, alert); !errors.Is(err, ErrStaleDraft) {
		t.Fatal("old revision confirmed draft")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- database.ConfirmDraft(42, "NATURAL_ALERT", raw, alert) }()
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
	alerts, _ := database.GetAllAlerts()
	process, _ := database.GetProcess(42)
	if successes != 1 || len(alerts) != 1 || process != nil || alerts[0].OriginalPrompt != alert.OriginalPrompt {
		t.Fatal("confirmation not atomic")
	}
}

func TestFailedConfirmationRetainsDraftAndStateSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"revision":3,"messages":["کرمان"]}`
	_ = database.UpsertProcess(42, "NATURAL_ALERT", raw, 1)
	invalid := Alert{Id: 1, ChatId: 42, Title: "عنوان", Interval: 1}
	if err = database.ConfirmDraft(42, "NATURAL_ALERT", raw, invalid); err == nil {
		t.Fatal("invalid alert inserted")
	}
	process, _ := database.GetProcess(42)
	if process == nil || process.ProcessData != raw {
		t.Fatal("failed insert consumed draft")
	}
	invalid.Interval = 60
	invalid.SearchQuery = `{"city_ids":["13"]}`
	if err = database.ConfirmDraft(42, "NATURAL_ALERT", raw, invalid); err != nil {
		t.Fatal(err)
	}
	if err = database.UpdateWatchState(1, `{"initialized":true,"cursor":{"opaque":42}}`, 10); err != nil {
		t.Fatal(err)
	}
	database.Close()
	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	alerts, _ := database.GetAllAlerts()
	if alerts[0].WatchState != `{"initialized":true,"cursor":{"opaque":42}}` {
		t.Fatal("scan checkpoint lost on restart")
	}
	if err = database.ResetAlert(42, 1); err != nil {
		t.Fatal(err)
	}
	alerts, _ = database.GetAllAlerts()
	if alerts[0].WatchState != "{}" {
		t.Fatal("fresh start retained old checkpoint")
	}
}
