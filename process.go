package main

import (
	"encoding/json"
	"github.com/dgraph-io/badger/v4"
	"strconv"
	"time"
)

// Step represents a single step in a process.
// Each step contains a name, associated data, and a message to be displayed.
//
// Fields:
//
//	Name (string): The name of the step.
//	Data (string): The data associated with the step.
//	Message (string): The message to be displayed for the step.
type Step struct {
	Name    string `json:"name"`
	Data    string `json:"data"`
	Message string `json:"message"`
}

// Process represents a multistep process for a user.
// It includes metadata, steps, and the current state of the process.
//
// Fields:
//
//	Id (string): The unique identifier for the process.
//	Step ([]Step): The list of steps in the process.
//	CurrentStepIndex (int): The index of the current step in the process.
//	ChatId (int64): The ID of the chat associated with the process.
//	LastActionAt (int64): The timestamp of the last action performed in the process.
type Process struct {
	Id               string `json:"id"`
	Step             []Step `json:"step"`
	CurrentStepIndex int    `json:"currentStepIndex"`
	ChatId           int64  `json:"chatId"`
	LastActionAt     int64  `json:"lastActionAt"`
}

// ProcessKey holds predefined keys for different processes.
//
// Fields:
//
//	SetAlert (string): The key for the "SET_ALERT" process.
var ProcessKey = struct {
	SetAlert string
}{
	SetAlert: "SET_ALERT",
}

// setAlertEmpty initializes a new "SET_ALERT" process with predefined steps.
//
// Parameters:
//
//	chatId (int64): The ID of the chat for which the process is being created.
//
// Returns:
//
//	Process: A new "SET_ALERT" process with predefined steps and metadata.
func setAlertEmpty(chatId int64) Process {
	return Process{
		Id: ProcessKey.SetAlert,
		Step: []Step{
			{
				Name:    "title",
				Data:    "",
				Message: "لطفا عنوان اعلان را ارسال کنید:",
			},
			{
				Name:    "link",
				Data:    "",
				Message: "لطفا لینک دیوار را ارسال کنید:",
			},
			{
				Name:    "interval",
				Data:    "",
				Message: "هر چند ثانیه میخواهید چک شود؟",
			},
			{
				Name:    "end",
				Data:    "",
				Message: "اعلان با موفقیت تنظیم شد.",
			},
		},
		CurrentStepIndex: 0,
		ChatId:           chatId,
		LastActionAt:     time.Now().Unix(),
	}
}

// setAlertOnComplete saves the completed "SET_ALERT" process to the database.
//
// Parameters:
//
//	p (Process): The completed process.
//
// Returns:
//
//	error: An error if the operation fails, otherwise nil.
func setAlertOnComplete(p Process) error {
	interval, err := strconv.Atoi(p.Step[2].Data)
	if err != nil {
		return err
	}

	return db.Update(func(txn *badger.Txn) error {
		alert := Alert{
			Id:       time.Now().UnixNano(),
			Title:    p.Step[0].Data,
			Link:     p.Step[1].Data,
			Interval: interval,
			ChatId:   p.ChatId,
		}

		key := "alert-" + strconv.FormatInt(p.ChatId, 10) + "-" + strconv.FormatInt(alert.Id, 10)

		value, err := json.Marshal(alert)
		if err != nil {
			return err
		}
		return txn.Set([]byte(key), value)
	})
}

// preStartNewProcess prepares a new process by deleting previous processes
// and setting the current process key in the database.
//
// Parameters:
//
//	processKey (string): The key of the process to be started.
//	chatId (int64): The ID of the chat for which the process is being started.
//	db (*badger.DB): The Badger database instance.
//
// Returns:
//
//	error: An error if the operation fails, otherwise nil.
func preStartNewProcess(processKey string, chatId int64, db *badger.DB) error {
	// delete previous processes for this user
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
		return err
	}

	// set current process for this user
	err = db.Update(func(txn *badger.Txn) error {
		key := []byte(strconv.FormatInt(chatId, 10) + "-CURRENT_PROCESS")
		value := []byte(processKey)
		return txn.Set(key, value)
	})
	if err != nil {
		return err
	}

	return nil
}

// processOnComplete handles the completion of a process by saving it and cleaning up related keys.
//
// Parameters:
//
//	p (Process): The completed process.
//	db (*badger.DB): The Badger database instance.
//
// Returns:
//
//	error: An error if the operation fails, otherwise nil.
func processOnComplete(p Process, db *badger.DB) error {
	var err error
	switch p.Id {
	case ProcessKey.SetAlert:
		err = setAlertOnComplete(p)
	default:
		return nil
	}

	if err != nil {
		return err
	}

	// delete current process key
	err = db.Update(func(txn *badger.Txn) error {
		key := []byte(strconv.FormatInt(p.ChatId, 10) + "-CURRENT_PROCESS")
		return txn.Delete(key)
	})
	if err != nil {
		return err
	}
	// delete process key
	err = db.Update(func(txn *badger.Txn) error {
		processKey := []byte(strconv.FormatInt(p.ChatId, 10) + "-" + p.Id)
		return txn.Delete(processKey)
	})
	if err != nil {
		return err
	}

	return nil
}

// ProcessStart initializes and starts a new process based on the given key.
//
// Parameters:
//
//	key (string): The key of the process to be started.
//	chatId (int64): The ID of the chat for which the process is being started.
//	db (*badger.DB): The Badger database instance.
//
// Returns:
//
//	Process: The initialized process.
//	error: An error if the operation fails, otherwise nil.
func ProcessStart(key string, chatId int64, db *badger.DB) (Process, error) {
	var p Process
	var err error

	// pre-start new process
	err = preStartNewProcess(key, chatId, db)
	if err != nil {
		return Process{}, err
	}

	switch key {
	case ProcessKey.SetAlert:
		p = setAlertEmpty(chatId)
	default:
		return Process{}, nil
	}

	// save process struct as json string to db
	err = db.Update(func(txn *badger.Txn) error {
		userProcessKey := []byte(strconv.FormatInt(chatId, 10) + "-" + key)
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

// ProcessGoNextStep advances the process to the next step, saves the updated process,
// and optionally calls the onComplete function if the process is completed.
//
// Parameters:
//
//	p (Process): The current process.
//	userInput (string): The user input for the current step.
//	db (*badger.DB): The Badger database instance.
//
// Returns:
//
//	Process: The updated process.
//	error: An error if the operation fails, otherwise nil.
func ProcessGoNextStep(p Process, userInput string, db *badger.DB) (Process, error) {
	var err error

	if p.CurrentStepIndex < len(p.Step)-1 {
		p.Step[p.CurrentStepIndex].Data = userInput
		p.CurrentStepIndex++
		p.LastActionAt = time.Now().Unix()

		// save process struct as json string to db
		err = db.Update(func(txn *badger.Txn) error {
			userProcessKey := []byte(strconv.FormatInt(p.ChatId, 10) + "-" + p.Id)
			value, err := json.Marshal(p)
			if err != nil {
				return err
			}
			return txn.Set(userProcessKey, value)
		})
		if err != nil {
			return Process{}, err
		}

		if p.CurrentStepIndex == len(p.Step)-1 {
			err = processOnComplete(p, db)
			if err != nil {
				return Process{}, err
			}
		}

		return p, nil
	}

	return Process{}, err
}

// CurrentProcess retrieves the current process for a given chat ID.
//
// Parameters:
//
//	chatId (int64): The ID of the chat.
//	db (*badger.DB): The Badger database instance.
//
// Returns:
//
//	string: The key of the current process.
//	Process: The current process.
//	error: An error if the operation fails, otherwise nil.
func CurrentProcess(chatId int64, db *badger.DB) (string, Process, error) {
	var processKey string
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(strconv.FormatInt(chatId, 10) + "-CURRENT_PROCESS"))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			processKey = string(val)
			return nil
		})
	})
	if err != nil {
		return "", Process{}, err
	}

	var p Process
	err = db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(strconv.FormatInt(chatId, 10) + "-" + processKey))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			if err := json.Unmarshal(val, &p); err != nil {
				return err
			}
			return nil
		})
	})
	if err != nil {
		return "", Process{}, err
	}

	return processKey, p, nil
}
