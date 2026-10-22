package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
)

func TestDelayedSearchCannotDeliverOrSaveAfterDeleteOrReset(t *testing.T) {
	for _, reset := range []bool{false, true} {
		name := "delete"
		if reset {
			name = "reset"
		}
		t.Run(name, func(t *testing.T) {
			database, err := db.Open(filepath.Join(t.TempDir(), "watch.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			f := newWatchFixture(t)
			f.alert.WatchState = `{"initialized":true,"watermark":"2026-01-01T00:00:00Z"}`
			if err := database.InsertAlert(f.alert); err != nil {
				t.Fatal(err)
			}
			f.watcher.Guard = database.WithActiveAlert
			f.watcher.Seen = database.PostExists
			f.watcher.Record = database.InsertPost
			f.watcher.Save = func(id int64, s string, checked int64) error {
				return database.UpdateWatchStateForGeneration(id, f.alert.Generation, s, checked)
			}
			entered := make(chan struct{})
			release := make(chan struct{})
			f.watcher.Search = func(context.Context, divar.SearchQuery) (divar.SearchPage, error) {
				close(entered)
				<-release
				return divar.SearchPage{Listings: []divar.Listing{listing("new", time.Now())}}, nil
			}
			done := make(chan error, 1)
			go func() { done <- f.watcher.Poll(context.Background(), f.alert) }()
			<-entered
			if reset {
				err = database.ResetAlert(f.alert.ChatId, f.alert.Id)
			} else {
				err = database.DeleteAlert(f.alert.ChatId, f.alert.Id)
			}
			if err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-done; !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("stale poll error=%v", err)
			}
			if len(f.sent) != 0 {
				t.Fatalf("late sends=%v", f.sent)
			}
			if seen, err := database.PostExists("new", f.alert.Id); err != nil || seen {
				t.Fatalf("late delivery recorded: %t %v", seen, err)
			}
			if reset {
				alerts, err := database.GetAllAlerts()
				if err != nil || len(alerts) != 1 || alerts[0].WatchState != "{}" || alerts[0].LastTimeChecked != 0 {
					t.Fatalf("reset state overwritten: %+v %v", alerts, err)
				}
			}
		})
	}
}
