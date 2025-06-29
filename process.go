package main

import (
	"encoding/json"
	"github.com/dgraph-io/badger/v4"
	"strconv"
	"time"
)

type Step struct {
	Name    string `json:"name"`
	Data    string `json:"data"`
	Message string `json:"message"`
}
type Process struct {
	Id               string `json:"id"`
	Step             []Step `json:"step"`
	CurrentStepIndex int    `json:"currentStepIndex"`
	ChatId           int64  `json:"chatId"`
	LastActionAt     int64  `json:"lastActionAt"`
}

func ProcessStartSetAlert(chatId int64, db *badger.DB) (Process, error) {
	processKey := "SET_ALERT"
	var userProcessKey []byte

	// remove all keys that start with chatId-SET_ALERT
	// remove all keys that start with chatId-SET_ALERT
	err := db.Update(func(txn *badger.Txn) error {
		prefix := []byte(strconv.FormatInt(chatId, 10) + "-" + processKey)
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			if err := txn.Delete(item.Key()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Process{}, err
	}

	p := Process{
		Id: "SET_ALERT",
		Step: []Step{
			{
				Name:    "sendLink",
				Data:    "",
				Message: "لطفا لینک دیوار را ارسال کنید:",
			},
			{
				Name:    "checkSeconds",
				Data:    "",
				Message: "هر چند ثانیه میخواهید چک شود؟",
			},
			{
				Name:    "end",
				Data:    "",
				Message: "هشدار با موفقیت تنظیم شد.",
			},
		},
		CurrentStepIndex: 0,
		ChatId:           chatId,
		LastActionAt:     time.Now().Unix(),
	}

	// set user current process to SET_ALERT
	err = db.Update(func(txn *badger.Txn) error {
		key := []byte(strconv.FormatInt(chatId, 10) + "-CURRENT_PROCESS")
		value := []byte(processKey)
		return txn.Set(key, value)
	})
	if err != nil {
		return Process{}, err
	}

	// save process struct as json string to db
	err = db.Update(func(txn *badger.Txn) error {
		userProcessKey = []byte(strconv.FormatInt(chatId, 10) + "-" + processKey)
		value, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return txn.Set(userProcessKey, value)
	})
	if err != nil {
		return Process{}, err
	}

	return p, nil
}
