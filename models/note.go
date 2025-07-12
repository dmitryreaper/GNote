package models

import (
	"time"
)

// Note представляет собой структуру заметки
type Note struct {
	ID          int          `json:"id"`
	Title       string       `json:"title"`
	Content     string       `json:"content"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	ReminderAt  *time.Time   `json:"reminder_at"` // Указатель для nullable поля
	Tags        []string     `json:"tags"`        // Теги заметки
	Attachments []Attachment `json:"attachments"` // Вложения заметки
}

// Attachment представляет собой структуру вложения к заметке
type Attachment struct {
	ID         int       `json:"id"`
	NoteID     int       `json:"note_id"`
	Filename   string    `json:"filename"`
	Filepath   string    `json:"filepath"` // Путь к файлу на диске
	MimeType   string    `json:"mime_type"`
	SizeBytes  int64     `json:"size_bytes"`
	UploadedAt time.Time `json:"uploaded_at"` // Время загрузки вложения
}
