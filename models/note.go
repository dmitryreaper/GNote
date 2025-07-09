package models

import (
	"time"
)

type Note struct {
	ID         int        `json:"id"`
	Title      string     `json:"title"`
	Content    string     `json:"content"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ReminderAt *time.Time `json:"reminder_at"`
	Tags       []string   `json:"tags"`
	Attachments []Attachment `json:"attachments"` 
}

// структура вложения
type Attachment struct {
	ID        int        `json:"id"`
	NoteID    int        `json:"note_id"`
	Filename  string     `json:"filename"`
	Filepath  string     `json:"filepath"` // путь на диске
	MimeType  string     `json:"mime_type"`
	SizeBytes int64      `json:"size_bytes"`
	UploadedAt time.Time `json:"uploaded_at"`
}
