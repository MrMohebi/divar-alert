package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestDeletedAlertIsRetainedButInactive(t *testing.T) {
	database := openTestDB(t, "soft-delete")
	alert := Alert{Id: 1, ChatId: 42, Title: "car", Link: "curl", Interval: 60}
	if err := database.InsertAlert(alert); err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteAlert(99, 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if err := database.DeleteAlert(42, 1); err != nil {
		t.Fatal(err)
	}
	var deleted int64
	var title string
	if err := database.conn.QueryRow(`SELECT deleted_at,title FROM alerts WHERE id=1`).Scan(&deleted, &title); err != nil || deleted == 0 || title != "car" {
		t.Fatalf("retention: %d %s %v", deleted, title, err)
	}
	all, err := database.GetAllAlerts()
	if err != nil || len(all) != 0 {
		t.Fatalf("scheduler: %+v %v", all, err)
	}
	own, err := database.GetAlertsByChatId(42)
	if err != nil || len(own) != 0 {
		t.Fatalf("menu: %+v %v", own, err)
	}
	for _, call := range []func() error{
		func() error { return database.DeleteAlert(42, 1) },
		func() error { return database.ResetAlert(42, 1) },
		func() error { return database.UpdateAlertAIEnabled(42, 1, true) },
		func() error { return database.UpdateAlertAnalysis(42, 1, "new") },
		func() error { return database.UpdateAlertLastChecked(1, 99) },
		func() error { return database.UpdateWatchState(1, `{}`, 99) },
		func() error {
			return database.WithActiveAlert(alert, func() error { t.Error("deleted alert executed callback"); return nil })
		},
	} {
		if err := call(); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("deleted mutation: %v", err)
		}
	}
}

func TestResetRetainsDeliveriesAndInvalidatesOldWork(t *testing.T) {
	database := openTestDB(t, "audit")
	alert := Alert{Id: 1, ChatId: 42, Title: "car", Interval: 60}
	if err := database.InsertAlert(alert); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertPost("a", 1, map[string]string{"title": "first"}, 123); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertPost("a", 1, nil, 124); err == nil {
		t.Fatal("duplicate active delivery accepted")
	}
	if err := database.ResetAlert(42, 1); err != nil {
		t.Fatal(err)
	}
	if err := database.WithActiveAlert(alert, func() error { t.Error("old generation delivered"); return nil }); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if err := database.UpdateWatchStateForGeneration(1, 0, `{"initialized":true}`, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if err := database.UpdateAlertLastCheckedForGeneration(1, 0, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	current, err := database.GetAllAlerts()
	if err != nil || len(current) != 1 || current[0].Generation != 1 || current[0].WatchState != "{}" {
		t.Fatalf("%+v %v", current, err)
	}
	if err := database.WithActiveAlert(current[0], func() error { return database.InsertPost("a", 1, nil, 456) }); err != nil {
		t.Fatal(err)
	}
	rows, err := database.conn.Query(`SELECT id,created_at,deleted_at FROM seen_posts ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	var lastID int64
	for rows.Next() {
		var id, created int64
		var deleted sql.NullInt64
		if err := rows.Scan(&id, &created, &deleted); err != nil {
			t.Fatal(err)
		}
		if id <= lastID || (n == 0 && (created != 123 || !deleted.Valid)) || (n == 1 && (created != 456 || deleted.Valid)) {
			t.Fatalf("invalid audit row %d %d %+v", id, created, deleted)
		}
		lastID = id
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("deliveries=%d", n)
	}
}

func TestLegacyDeliveriesMigrationAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(`CREATE TABLE alerts(id INTEGER PRIMARY KEY,chat_id INTEGER NOT NULL,title TEXT NOT NULL,link TEXT NOT NULL,ai_enabled BOOLEAN NOT NULL DEFAULT true,analysis_prompt TEXT NOT NULL DEFAULT '',interval_seconds INTEGER NOT NULL,last_time_checked INTEGER NOT NULL);
 INSERT INTO alerts VALUES(1,42,'car','curl',true,'',60,100);
 CREATE TABLE seen_posts(token TEXT NOT NULL,alert_id INTEGER NOT NULL,data TEXT,created_at INTEGER NOT NULL,PRIMARY KEY(token,alert_id),FOREIGN KEY(alert_id) REFERENCES alerts(id) ON DELETE CASCADE);
 INSERT INTO seen_posts VALUES('old',1,'{"title":"old"}',123);`)
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	for i := 0; i < 2; i++ {
		database, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var gotID, created int64
		var data string
		var deleted sql.NullInt64
		err = database.conn.QueryRow(`SELECT id,data,created_at,deleted_at FROM seen_posts WHERE token='old'`).Scan(&gotID, &data, &created, &deleted)
		if err != nil || gotID == 0 || data != `{"title":"old"}` || created != 123 || deleted.Valid {
			t.Fatalf("migration %d %s %d %+v %v", gotID, data, created, deleted, err)
		}
		if i == 0 {
			id = gotID
			if err := database.DeleteAlert(42, 1); err != nil {
				t.Fatal(err)
			}
		} else {
			if id != gotID {
				t.Fatal("delivery ID changed on reopen")
			}
			a, _ := database.GetAllAlerts()
			if len(a) != 0 {
				t.Fatal("deleted alert reactivated")
			}
		}
		database.Close()
	}
}

func TestDeleteAndResetWaitForDelivery(t *testing.T) {
	for _, reset := range []bool{false, true} {
		name := "delete"
		if reset {
			name = "reset"
		}
		t.Run(name, func(t *testing.T) {
			database := openTestDB(t, name)
			alert := Alert{Id: 1, ChatId: 42, Interval: 60}
			if err := database.InsertAlert(alert); err != nil {
				t.Fatal(err)
			}
			entered := make(chan struct{})
			release := make(chan struct{})
			delivered := make(chan error, 1)
			go func() {
				delivered <- database.WithActiveAlert(alert, func() error { close(entered); <-release; return database.InsertPost("a", 1, nil, 123) })
			}()
			<-entered
			finished := make(chan error, 1)
			started := make(chan struct{})
			go func() {
				close(started)
				if reset {
					finished <- database.ResetAlert(42, 1)
				} else {
					finished <- database.DeleteAlert(42, 1)
				}
			}()
			<-started
			select {
			case err := <-finished:
				t.Fatalf("mutation passed active delivery: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			if err := <-delivered; err != nil {
				t.Fatal(err)
			}
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			if err := database.WithActiveAlert(alert, func() error { t.Error("late send after acknowledgment"); return nil }); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal(err)
			}
			var count int
			if err := database.conn.QueryRow(`SELECT COUNT(*) FROM seen_posts`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("delivery lost: %d %v", count, err)
			}
		})
	}
}
