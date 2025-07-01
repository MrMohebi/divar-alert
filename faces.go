package main

type Alert struct {
	Id              int64  `json:"id"`
	Title           string `json:"title"`
	Link            string `json:"link"`
	Interval        int    `json:"interval"` // in seconds
	ChatId          int64  `json:"chatId"`
	LastTimeChecked int64  `json:"lastTimeChecked"` // timestamp of the last check
}
