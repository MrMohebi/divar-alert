package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
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
	AlertId          int64  `json:"alertId,omitempty"`
	LastActionAt     int64  `json:"lastActionAt"`
}

// ProcessKey holds predefined keys for different processes.
//
// Fields:
//
//	SetAlert (string): The key for the "SET_ALERT" process.
var ProcessKey = struct {
	SetAlert    string
	SetAIPrompt string
}{
	SetAlert:    "SET_ALERT",
	SetAIPrompt: "SET_AI_PROMPT",
}

func setAIPromptEmpty(chatId, alertId int64) Process {
	return Process{
		Id: ProcessKey.SetAIPrompt,
		Step: []Step{
			{Name: "analysis_prompt", Message: "برای فعال کردن تحلیل هوش مصنوعی، شرایط بررسی آگهی را بنویسید:"},
			{Name: "end", Message: "تحلیل هوش مصنوعی برای اعلان فعال شد."},
		},
		CurrentStepIndex: 0,
		ChatId:           chatId,
		AlertId:          alertId,
		LastActionAt:     time.Now().Unix(),
	}
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
				Message: "دستور curl جستجوی دیوار را بفرستید. در مرورگر وارد صفحه جستجو شوید، در DevTools بخش Network درخواست search را پیدا کنید و Copy as cURL را بزنید:",
			},
			{
				Name:    "ai_enabled",
				Data:    "",
				Message: "تحلیل هوش مصنوعی برای این اعلان فعال باشد؟",
			},
			{
				Name:    "analysis_prompt",
				Data:    "",
				Message: "می‌خواهید هوش مصنوعی آگهی‌ها را بر چه اساسی بررسی کند؟ مثال: فقط خودروهای بدون رنگ با قیمت مناسب را پیشنهاد بده.",
			},
			{
				Name:    "interval",
				Data:    "",
				Message: "فاصله بررسی را انتخاب کنید یا عددی بین 10 تا 86400 ثانیه بفرستید:",
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
	intervalData, err := getStepData(p, "interval")
	if err != nil {
		return err
	}
	interval, err := strconv.Atoi(intervalData)
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

	title, err := getStepData(p, "title")
	if err != nil {
		return err
	}
	link, err := getStepData(p, "link")
	if err != nil {
		return err
	}
	analysisPrompt, _ := getStepData(p, "analysis_prompt")
	aiEnabled := true
	if aiEnabledData, aiErr := getStepData(p, "ai_enabled"); aiErr == nil {
		aiEnabled, err = strconv.ParseBool(aiEnabledData)
		if err != nil {
			return err
		}
	}

	alert := db.Alert{
		Id:              time.Now().UnixNano(),
		Title:           title,
		Link:            link,
		AIEnabled:       aiEnabled,
		AnalysisPrompt:  analysisPrompt,
		Interval:        interval,
		ChatId:          p.ChatId,
		LastTimeChecked: time.Now().Unix(),
	}

	return database.InsertAlert(alert)
}

func getStepData(p Process, name string) (string, error) {
	for _, step := range p.Step {
		if step.Name == name {
			return step.Data, nil
		}
	}
	return "", fmt.Errorf("process step %q not found", name)
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
	case ProcessKey.SetAIPrompt:
		prompt, promptErr := getStepData(p, "analysis_prompt")
		if promptErr != nil {
			return promptErr
		}
		err = database.UpdateAlertAnalysis(p.ChatId, p.AlertId, prompt)
	default:
		return nil
	}

	if err != nil {
		return err
	}

	// Delete the process from the database
	return database.DeleteProcess(p.ChatId)
}

func ProcessStartAIPrompt(chatId, alertId int64, database *db.DB) (Process, error) {
	if err := preStartNewProcess(ProcessKey.SetAIPrompt, chatId, database); err != nil {
		return Process{}, err
	}
	process := setAIPromptEmpty(chatId, alertId)
	processData, err := json.Marshal(process)
	if err != nil {
		return Process{}, err
	}
	if err := database.UpsertProcess(chatId, process.Id, string(processData), process.LastActionAt); err != nil {
		return Process{}, err
	}
	return process, nil
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
		validatedInput, err := validateStepInput(p.Step[p.CurrentStepIndex].Name, userInput)
		if err != nil {
			return p, err
		}

		p.Step[p.CurrentStepIndex].Data = validatedInput
		p.CurrentStepIndex++
		if p.Step[p.CurrentStepIndex-1].Name == "ai_enabled" && validatedInput == "false" && p.CurrentStepIndex < len(p.Step)-1 && p.Step[p.CurrentStepIndex].Name == "analysis_prompt" {
			p.Step[p.CurrentStepIndex].Data = ""
			p.CurrentStepIndex++
		}
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

func validateStepInput(stepName, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("input cannot be empty")
	}

	switch stepName {
	case "ai_enabled":
		if input != "true" && input != "false" {
			return "", errors.New("choose whether AI analysis is enabled")
		}
	case "link":
		if err := divar.ValidateSearchCommand(input); err != nil {
			return "", err
		}
	case "interval":
		interval, err := strconv.Atoi(input)
		if err != nil {
			return "", errors.New("interval must be a number")
		}
		if interval < 10 || interval > 86400 {
			return "", errors.New("interval must be between 10 and 86400 seconds")
		}
	case "analysis_prompt":
		if len([]rune(input)) > 4000 {
			return "", errors.New("analysis prompt must be at most 4000 characters")
		}
	}

	return input, nil
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
