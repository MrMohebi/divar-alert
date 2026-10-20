package db

import (
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T, name string) *DB {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), name+".db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestUpsertProcessPreservesExistingStateOnInvalidUpdate(t *testing.T) {
	database := openTestDB(t, "process")

	if err := database.UpsertProcess(42, "SET_ALERT", `{"step":1}`, 100); err != nil {
		t.Fatalf("initial UpsertProcess() error = %v", err)
	}
	if err := database.UpsertProcess(42, "SET_ALERT", `not-json`, 200); err == nil {
		t.Fatal("UpsertProcess() error = nil for invalid JSON")
	}

	process, err := database.GetProcess(42)
	if err != nil {
		t.Fatalf("GetProcess() error = %v", err)
	}
	if process == nil || process.ProcessData != `{"step":1}` || process.LastActionAt != 100 {
		t.Fatalf("process after failed update = %#v", process)
	}
}

func TestAlertSettingsCheckOwnership(t *testing.T) {
	database := openTestDB(t, "settings")
	alert := Alert{Id: 1, ChatId: 2, Title: "cars", Link: "curl", AIEnabled: true, Interval: 60}
	if err := database.InsertAlert(alert); err != nil {
		t.Fatalf("InsertAlert() error = %v", err)
	}
	if err := database.UpdateAlertAIEnabled(999, alert.Id, false); err == nil {
		t.Fatal("UpdateAlertAIEnabled() allowed another chat to change the alert")
	}
	if err := database.UpdateAlertAIEnabled(alert.ChatId, alert.Id, false); err != nil {
		t.Fatalf("UpdateAlertAIEnabled() error = %v", err)
	}
	if err := database.UpdateAlertAnalysis(alert.ChatId, alert.Id, "only cheap cars"); err != nil {
		t.Fatalf("UpdateAlertAnalysis() error = %v", err)
	}

	alerts, err := database.GetAlertsByChatId(alert.ChatId)
	if err != nil || len(alerts) != 1 || !alerts[0].AIEnabled || alerts[0].AnalysisPrompt != "only cheap cars" {
		t.Fatalf("alerts = %#v, error = %v", alerts, err)
	}
	if err := database.DeleteAlert(999, alert.Id); err == nil {
		t.Fatal("DeleteAlert() allowed another chat to delete the alert")
	}
}

func TestHistoryPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	database, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	alert := Alert{Id: 1, ChatId: 2, Title: "cars", Link: "curl", Interval: 60}
	if err := database.InsertAlert(alert); err != nil {
		t.Fatalf("InsertAlert() error = %v", err)
	}
	if err := database.AppendHistory(alert.Id, map[string]interface{}{"title": "first", "price": float64(10)}); err != nil {
		t.Fatalf("AppendHistory(first) error = %v", err)
	}
	if err := database.AppendHistory(alert.Id, map[string]interface{}{"title": "second"}); err != nil {
		t.Fatalf("AppendHistory(second) error = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	database, err = Open(path)
	if err != nil {
		t.Fatalf("Open() after close error = %v", err)
	}
	t.Cleanup(func() { database.Close() })
	history, err := database.LoadHistory(alert.Id)
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(history) != 2 || history[0].(map[string]interface{})["title"] != "first" || history[1].(map[string]interface{})["title"] != "second" {
		t.Fatalf("history = %#v", history)
	}
}

func TestResetAlertClearsHistoryAndSeenPosts(t *testing.T) {
	database := openTestDB(t, "reset")
	alert := Alert{Id: 1, ChatId: 2, Title: "cars", Link: "curl", Interval: 60, LastTimeChecked: 123}
	if err := database.InsertAlert(alert); err != nil {
		t.Fatalf("InsertAlert() error = %v", err)
	}
	if err := database.InsertPost("token", alert.Id, map[string]string{"title": "car"}, 100); err != nil {
		t.Fatalf("InsertPost() error = %v", err)
	}
	if err := database.AppendHistory(alert.Id, map[string]string{"title": "car"}); err != nil {
		t.Fatalf("AppendHistory() error = %v", err)
	}
	if err := database.ResetAlert(999, alert.Id); err == nil {
		t.Fatal("ResetAlert() allowed another chat to reset the alert")
	}
	exists, err := database.PostExists("token", alert.Id)
	if err != nil || !exists {
		t.Fatalf("post after rejected reset: exists = %t, error = %v", exists, err)
	}

	if err := database.ResetAlert(alert.ChatId, alert.Id); err != nil {
		t.Fatalf("ResetAlert() error = %v", err)
	}
	exists, err = database.PostExists("token", alert.Id)
	if err != nil || exists {
		t.Fatalf("post after reset: exists = %t, error = %v", exists, err)
	}
	history, err := database.LoadHistory(alert.Id)
	if err != nil || len(history) != 0 {
		t.Fatalf("history after reset = %#v, error = %v", history, err)
	}
	alerts, err := database.GetAlertsByChatId(alert.ChatId)
	if err != nil || len(alerts) != 1 || alerts[0].LastTimeChecked != 0 {
		t.Fatalf("alert after reset = %#v, error = %v", alerts, err)
	}
}

func TestDeleteAlertCascadesStoredPosts(t *testing.T) {
	database := openTestDB(t, "delete")
	alert := Alert{Id: 1, ChatId: 2, Title: "cars", Link: "curl", Interval: 60}
	if err := database.InsertAlert(alert); err != nil {
		t.Fatalf("InsertAlert() error = %v", err)
	}
	if err := database.InsertPost("token", alert.Id, nil, 100); err != nil {
		t.Fatalf("InsertPost() error = %v", err)
	}
	if err := database.AppendHistory(alert.Id, map[string]string{"title": "car"}); err != nil {
		t.Fatalf("AppendHistory() error = %v", err)
	}
	if err := database.DeleteAlert(alert.ChatId, alert.Id); err != nil {
		t.Fatalf("DeleteAlert() error = %v", err)
	}

	for _, table := range []string{"seen_posts", "alert_history"} {
		var count int
		if err := database.conn.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count = %d, error = %v", table, count, err)
		}
	}
}

func TestHistoryRetention(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "append"
		if legacy {
			name = "reopen legacy history"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "retention.db")
			database, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { database.Close() }()
			for _, id := range []int64{1, 2} {
				if err := database.InsertAlert(Alert{Id: id, ChatId: 2, Title: "cars", Link: "curl", Interval: 60}); err != nil {
					t.Fatal(err)
				}
			}
			if err := database.InsertPost("old-delivery", 1, nil, 1); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 65; i++ {
				if legacy {
					_, err = database.conn.Exec(`INSERT INTO alert_history (alert_id, data, created_at) VALUES (1, ?, 1)`, i)
				} else {
					err = database.AppendHistory(1, i)
				}
				if err != nil {
					t.Fatal(err)
				}
				if i < 7 {
					if err := database.AppendHistory(2, i); err != nil {
						t.Fatal(err)
					}
				}
			}
			if !legacy {
				var stored int
				if err := database.conn.QueryRow(`SELECT COUNT(*) FROM alert_history WHERE alert_id = 1`).Scan(&stored); err != nil {
					t.Fatal(err)
				}
				if stored != 50 {
					t.Fatalf("stored history before restart = %d; want 50", stored)
				}
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			database, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, check := range []struct {
				id           int64
				count, first int
			}{{1, 50, 15}, {2, 7, 0}} {
				history, err := database.LoadHistory(check.id)
				if err != nil {
					t.Fatal(err)
				}
				var stored int
				if err := database.conn.QueryRow(`SELECT COUNT(*) FROM alert_history WHERE alert_id = ?`, check.id).Scan(&stored); err != nil {
					t.Fatal(err)
				}
				if stored != check.count || len(history) != check.count {
					t.Fatalf("alert %d: stored=%d, loaded=%d; want %d", check.id, stored, len(history), check.count)
				}
				for i, post := range history {
					if post != float64(check.first+i) {
						t.Fatalf("alert %d history[%d] = %v; want %d", check.id, i, post, check.first+i)
					}
				}
			}
			if exists, err := database.PostExists("old-delivery", 1); err != nil || !exists {
				t.Fatalf("delivery deduplication lost: exists=%t err=%v", exists, err)
			}
		})
	}
}
