package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestPlatformsWithSameChatIDAreIsolated(t *testing.T) {
	root := openTestDB(t, "platforms")
	telegram, bale := root.ForPlatform("telegram"), root.ForPlatform("bale")

	if err := telegram.InsertAlert(Alert{Id: 1, ChatId: 42, Title: "telegram cars", Link: "curl", Interval: 60}); err != nil {
		t.Fatal(err)
	}
	if err := bale.InsertAlert(Alert{Id: 2, ChatId: 42, Title: "bale cars", Link: "curl", Interval: 60}); err != nil {
		t.Fatal(err)
	}
	for _, view := range []*DB{telegram, bale} {
		alerts, err := view.GetAlertsByChatId(42)
		if err != nil || len(alerts) != 1 || alerts[0].Platform != view.Platform() {
			t.Fatalf("%s alerts=%+v err=%v", view.Platform(), alerts, err)
		}
	}
	if err := telegram.DeleteAlert(42, 2); err == nil {
		t.Fatal("a Telegram chat deleted a Bale alert with the same chat ID")
	}
	if err := telegram.ResetAlert(42, 2); err == nil {
		t.Fatal("a Telegram chat reset a Bale alert with the same chat ID")
	}
	if err := telegram.UpdateAlertAIEnabled(42, 2, false); err == nil {
		t.Fatal("a Telegram chat changed a Bale alert with the same chat ID")
	}

	if err := telegram.UpsertProcess(42, "NATURAL_ALERT", `{"from":"telegram"}`, 1); err != nil {
		t.Fatal(err)
	}
	if err := bale.UpsertProcess(42, "NATURAL_ALERT", `{"from":"bale"}`, 1); err != nil {
		t.Fatal(err)
	}
	if err := telegram.DeleteProcess(42); err != nil {
		t.Fatal(err)
	}
	if process, err := bale.GetProcess(42); err != nil || process == nil || process.ProcessData != `{"from":"bale"}` {
		t.Fatalf("bale draft=%+v err=%v", process, err)
	}
	if process, err := telegram.GetProcess(42); err != nil || process != nil {
		t.Fatalf("telegram draft=%+v err=%v", process, err)
	}

	// A draft can only be confirmed by its own platform.
	alert := Alert{Id: 3, ChatId: 42, Title: "draft", Link: "curl", Interval: 60}
	if err := telegram.ConfirmDraft(42, "NATURAL_ALERT", `{"from":"bale"}`, alert); err == nil {
		t.Fatal("Telegram confirmed a Bale draft")
	}
	if err := bale.ConfirmDraft(42, "NATURAL_ALERT", `{"from":"bale"}`, alert); err != nil {
		t.Fatal(err)
	}
	if alerts, _ := bale.GetAlertsByChatId(42); len(alerts) != 2 {
		t.Fatalf("bale alerts after confirm=%+v", alerts)
	}
}

func TestPreviousSingleBotDatabaseIsAdopted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "single-bot.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()

	// Recreate the schema saved before platform support: no platform columns,
	// and drafts keyed by chat_id alone.
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`DROP INDEX idx_alerts_platform_chat;
		ALTER TABLE alerts DROP COLUMN platform;
		DROP TABLE processes;
		CREATE TABLE processes (chat_id INTEGER PRIMARY KEY, process_key TEXT NOT NULL, process_data TEXT NOT NULL CHECK(json_valid(process_data)), last_action_at INTEGER NOT NULL);
		INSERT INTO alerts (id, chat_id, title, link, interval_seconds, last_time_checked) VALUES (1, 42, 'old', 'curl', 60, 0);
		INSERT INTO processes VALUES (42, 'NATURAL_ALERT', '{"old":true}', 5);`); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if n, err := database.CountUnscoped(); err != nil || n != 2 {
		t.Fatalf("unscoped=%d err=%v", n, err)
	}
	if n, err := database.AdoptUnscoped("bale"); err != nil || n != 2 {
		t.Fatalf("adopted=%d err=%v", n, err)
	}
	bale := database.ForPlatform("bale")
	if alerts, err := bale.GetAlertsByChatId(42); err != nil || len(alerts) != 1 || alerts[0].Title != "old" {
		t.Fatalf("bale alerts=%+v err=%v", alerts, err)
	}
	if process, err := bale.GetProcess(42); err != nil || process == nil || process.ProcessData != `{"old":true}` {
		t.Fatalf("bale draft=%+v err=%v", process, err)
	}
	if alerts, _ := database.ForPlatform("telegram").GetAlertsByChatId(42); len(alerts) != 0 {
		t.Fatalf("telegram sees adopted alerts: %+v", alerts)
	}
	if n, err := database.CountUnscoped(); err != nil || n != 0 {
		t.Fatalf("unscoped after adopt=%d err=%v", n, err)
	}
}
