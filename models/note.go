package models

import (
	"time"
)

// Note представляет собой структуру заметки
type Note struct {
	ID         int        `json:"id"`
	Title      string     `json:"title"`
	Content    string     `json:"content"`
	Tags       []string   `json:"tags"` 
	ReminderAt *time.Time `json:"reminder_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
