package main

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/mrmohebi/divar-alert/db"
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
//	database (*db.DB): The database instance.
//
// Returns:
//
//	error: An error if the operation fails, otherwise nil.
func setAlertOnComplete(p Process, database *db.DB) error {
	interval, err := strconv.Atoi(p.Step[2].Data)
	if err != nil {
		return err
	}

	// Validate interval - minimum 10 seconds to prevent API abuse
	if interval < 10 {
		return errors.New("interval must be at least 10 seconds")
	}

	// Validate interval - maximum 86400 seconds (24 hours) for reasonable limits
	if interval > 86400 {
		return errors.New("interval must be at most 86400 seconds (24 hours)")
	}

	alert := db.Alert{
		Id:              time.Now().UnixNano(),
		Title:           p.Step[0].Data,
		Link:            p.Step[1].Data,
		Interval:        interval,
		ChatId:          p.ChatId,
		LastTimeChecked: time.Now().Unix(),
	}

	return database.InsertAlert(alert)
}

// preStartNewProcess prepares a new process by deleting previous processes
// and setting the current process key in the database.
//
// Parameters:
//
//	processKey (string): The key of the process to be started.
//	chatId (int64): The ID of the chat for which the process is being started.
//	database (*db.DB): The database instance.
//
// Returns:
//
//	error: An error if the operation fails, otherwise nil.
func preStartNewProcess(processKey string, chatId int64, database *db.DB) error {
	// Delete any existing process for this user and set the new one
	return database.DeleteProcess(chatId)
}

// processOnComplete handles the completion of a process by saving it and cleaning up related keys.
//
// Parameters:
//
//	p (Process): The completed process.
//	database (*db.DB): The database instance.
//
// Returns:
//
//	error: An error if the operation fails, otherwise nil.
func processOnComplete(p Process, database *db.DB) error {
	var err error
	switch p.Id {
	case ProcessKey.SetAlert:
		err = setAlertOnComplete(p, database)
	default:
		return nil
	}

	if err != nil {
		return err
	}

	// Delete the process from the database
	return database.DeleteProcess(p.ChatId)
}

// ProcessStart initializes and starts a new process based on the given key.
//
// Parameters:
//
//	key (string): The key of the process to be started.
//	chatId (int64): The ID of the chat for which the process is being started.
//	database (*db.DB): The database instance.
//
// Returns:
//
//	Process: The initialized process.
//	error: An error if the operation fails, otherwise nil.
func ProcessStart(key string, chatId int64, database *db.DB) (Process, error) {
	var p Process

	// pre-start new process (delete any existing)
	err := preStartNewProcess(key, chatId, database)
	if err != nil {
		return Process{}, err
	}

	switch key {
	case ProcessKey.SetAlert:
		p = setAlertEmpty(chatId)
	default:
		return Process{}, nil
	}

	// Save process to database as JSON
	processData, err := json.Marshal(p)
	if err != nil {
		return Process{}, err
	}
	err = database.UpsertProcess(chatId, key, string(processData), p.LastActionAt)
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
//	database (*db.DB): The database instance.
//
// Returns:
//
//	Process: The updated process.
//	error: An error if the operation fails, otherwise nil.
func ProcessGoNextStep(p Process, userInput string, database *db.DB) (Process, error) {
	if p.CurrentStepIndex < len(p.Step)-1 {
		p.Step[p.CurrentStepIndex].Data = userInput
		p.CurrentStepIndex++
		p.LastActionAt = time.Now().Unix()

		// Save process to database as JSON
		processData, err := json.Marshal(p)
		if err != nil {
			return Process{}, err
		}

		err = database.UpsertProcess(p.ChatId, p.Id, string(processData), p.LastActionAt)
		if err != nil {
			return Process{}, err
		}

		if p.CurrentStepIndex == len(p.Step)-1 {
			err = processOnComplete(p, database)
			if err != nil {
				return Process{}, err
			}
		}

		return p, nil
	}

	return Process{}, errors.New("process already completed")
}

// CurrentProcess retrieves the current process for a given chat ID.
//
// Parameters:
//
//	chatId (int64): The ID of the chat.
//	database (*db.DB): The database instance.
//
// Returns:
//
//	string: The key of the current process.
//	Process: The current process.
//	error: An error if the operation fails, otherwise nil.
func CurrentProcess(chatId int64, database *db.DB) (string, Process, error) {
	proc, err := database.GetProcess(chatId)
	if err != nil {
		return "", Process{}, err
	}

	if proc == nil {
		return "", Process{}, errors.New("no process found")
	}

	var p Process
	if err := json.Unmarshal([]byte(proc.ProcessData), &p); err != nil {
		return "", Process{}, err
	}

	return proc.ProcessKey, p, nil
}
