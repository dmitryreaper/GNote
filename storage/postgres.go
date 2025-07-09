package storage

import (
	"database/sql"
	"fmt"
	"log"
	"time"
	"os"

	"github.com/lib/pq" 
	"GNote/models" 
)

// Config содержит конфигурацию для подключения к БД
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// Store представляет собой интерфейс для взаимодействия с заметками
type Store interface {
	CreateNote(note *models.Note) error
	GetNoteByID(id int) (*models.Note, error)
	GetAllNotes() ([]models.Note, error)
	UpdateNote(note *models.Note) error
	DeleteNote(id int) error
	CreateAttachment(attachment *models.Attachment) error
	GetAttachmentsByNoteID(noteID int) ([]models.Attachment, error)
	DeleteAttachment(attachmentID int) error
}

// PostgresStore реализует Store для PostgreSQL
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore создает новый экземпляр PostgresStore
func NewPostgresStore(cfg Config) (*PostgresStore, error) {
	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("ошибка при открытии соединения с БД: %w", err)
	}

	// Проверяем соединение
	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("ошибка при подключении к БД: %w", err)
	}

	log.Println("Успешное подключение к PostgreSQL!")
	return &PostgresStore{db: db}, nil
}

// CreateNote создает новую заметку в БД, включая теги и напоминания
func (s *PostgresStore) CreateNote(note *models.Note) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("не удалось начать транзакцию: %w", err)
	}
	defer tx.Rollback() // Откат в случае ошибки

	// Вставляем заметку
	query := `INSERT INTO notes (title, content, reminder_at) VALUES ($1, $2, $3) RETURNING id, created_at, updated_at`
	var reminderAtSQL sql.NullTime
	if note.ReminderAt != nil {
		reminderAtSQL = sql.NullTime{Time: *note.ReminderAt, Valid: true}
	}
	err = tx.QueryRow(query, note.Title, note.Content, reminderAtSQL).Scan(&note.ID, &note.CreatedAt, &note.UpdatedAt)
	if err != nil {
		return fmt.Errorf("ошибка при создании заметки: %w", err)
	}

	// Обрабатываем теги
	if len(note.Tags) > 0 {
		for _, tagName := range note.Tags {
			var tagID int
			// Ищем существующий тег или создаем новый
			err := tx.QueryRow(`INSERT INTO tags (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name=EXCLUDED.name RETURNING id`, tagName).Scan(&tagID)
			if err != nil {
				return fmt.Errorf("ошибка при создании/получении тега: %w", err)
			}
			// Привязываем тег к заметке
			_, err = tx.Exec(`INSERT INTO note_tags (note_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, note.ID, tagID)
			if err != nil {
				return fmt.Errorf("ошибка при привязке тега к заметке: %w", err)
			}
		}
	}

	return tx.Commit() // Подтверждаем транзакцию
}

// GetNoteByID получает заметку по ID, включая теги и вложения
func (s *PostgresStore) GetNoteByID(id int) (*models.Note, error) {
	var note models.Note
	var reminderAtSQL sql.NullTime

	query := `SELECT id, title, content, created_at, updated_at, reminder_at FROM notes WHERE id = $1`
	err := s.db.QueryRow(query, id).Scan(&note.ID, &note.Title, &note.Content, &note.CreatedAt, &note.UpdatedAt, &reminderAtSQL)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("заметка с ID %d не найдена", id)
		}
		return nil, fmt.Errorf("ошибка при получении заметки по ID: %w", err)
	}

	if reminderAtSQL.Valid {
		note.ReminderAt = &reminderAtSQL.Time
	}

	// Получаем теги для заметки
	rows, err := s.db.Query(`SELECT t.name FROM tags t JOIN note_tags nt ON t.id = nt.tag_id WHERE nt.note_id = $1`, note.ID)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении тегов заметки: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tagName string
		if err := rows.Scan(&tagName); err != nil {
			return nil, fmt.Errorf("ошибка при сканировании тега: %w", err)
		}
		tags = append(tags, tagName)
	}
	note.Tags = tags

	// Получаем вложения для заметки
	attachments, err := s.GetAttachmentsByNoteID(note.ID)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении вложений заметки: %w", err)
	}
	note.Attachments = attachments

	return &note, nil
}

// GetAllNotes получает все заметки, включая теги (вложения не загружаем для списка, чтобы не перегружать)
func (s *PostgresStore) GetAllNotes() ([]models.Note, error) {
	query := `
		SELECT
			n.id, n.title, n.content, n.created_at, n.updated_at, n.reminder_at,
			COALESCE(ARRAY_AGG(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags
		FROM notes n
		LEFT JOIN note_tags nt ON n.id = nt.note_id
		LEFT JOIN tags t ON nt.tag_id = t.id
		GROUP BY n.id, n.title, n.content, n.created_at, n.updated_at, n.reminder_at
		ORDER BY n.created_at DESC`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении всех заметок: %w", err)
	}
	defer rows.Close()

	var notes []models.Note
	for rows.Next() {
		var note models.Note
		var tagsArray pq.StringArray // <--- ИЗМЕНЕНИЕ ЗДЕСЬ: используем pq.StringArray
		var reminderAtSQL sql.NullTime

		if err := rows.Scan(&note.ID, &note.Title, &note.Content, &note.CreatedAt, &note.UpdatedAt, &reminderAtSQL, &tagsArray); err != nil {
			return nil, fmt.Errorf("ошибка при сканировании заметки: %w", err)
		}

		if reminderAtSQL.Valid {
			note.ReminderAt = &reminderAtSQL.Time
		}

		// Преобразуем pq.StringArray в []string
		note.Tags = []string(tagsArray) // <--- ИЗМЕНЕНИЕ ЗДЕСЬ: прямое преобразование
		// Вложения не загружаем здесь, только при выборе конкретной заметки
		note.Attachments = []models.Attachment{}
		notes = append(notes, note)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка после итерации по строкам: %w", err)
	}

	return notes, nil
}

// UpdateNote обновляет существующую заметку, включая теги и напоминания
func (s *PostgresStore) UpdateNote(note *models.Note) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("не удалось начать транзакцию: %w", err)
	}
	defer tx.Rollback()

	// Устанавливаем updated_at в Go, чтобы явно использовать пакет time
	note.UpdatedAt = time.Now()

	// Обновляем заметку
	query := `UPDATE notes SET title = $1, content = $2, reminder_at = $3, updated_at = $4 WHERE id = $5`
	var reminderAtSQL sql.NullTime
	if note.ReminderAt != nil {
		reminderAtSQL = sql.NullTime{Time: *note.ReminderAt, Valid: true}
	}
	res, err := tx.Exec(query, note.Title, note.Content, reminderAtSQL, note.UpdatedAt, note.ID)
	if err != nil {
		return fmt.Errorf("ошибка при обновлении заметки: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("ошибка при получении количества затронутых строк: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("заметка с ID %d не найдена для обновления", note.ID)
	}

	// Удаляем старые привязки тегов для этой заметки
	_, err = tx.Exec(`DELETE FROM note_tags WHERE note_id = $1`, note.ID)
	if err != nil {
		return fmt.Errorf("ошибка при удалении старых тегов: %w", err)
	}

	// Добавляем новые привязки тегов
	if len(note.Tags) > 0 {
		for _, tagName := range note.Tags {
			var tagID int
			err := tx.QueryRow(`INSERT INTO tags (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name=EXCLUDED.name RETURNING id`, tagName).Scan(&tagID)
			if err != nil {
				return fmt.Errorf("ошибка при создании/получении тега: %w", err)
			}
			_, err = tx.Exec(`INSERT INTO note_tags (note_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, note.ID, tagID)
			if err != nil {
				return fmt.Errorf("ошибка при привязке тега к заметке: %w", err)
			}
		}
	}

	return tx.Commit()
}

// DeleteNote удаляет заметку по ID
func (s *PostgresStore) DeleteNote(id int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("не удалось начать транзакцию: %w", err)
	}
	defer tx.Rollback()

	// Сначала получаем пути к файлам вложений, чтобы удалить их с диска
	attachments, err := s.GetAttachmentsByNoteID(id)
	if err != nil {
		// Логируем ошибку, но продолжаем удаление заметки, чтобы не блокировать
		log.Printf("Предупреждение: не удалось получить вложения для заметки ID %d при удалении: %v", id, err)
	}

	// Удаляем привязки тегов (CASCADE в БД позаботится об этом, но можно явно)
	_, err = tx.Exec(`DELETE FROM note_tags WHERE note_id = $1`, id)
	if err != nil {
		return fmt.Errorf("ошибка при удалении привязок тегов: %w", err)
	}

	// Удаляем вложения из таблицы attachments (благодаря ON DELETE CASCADE это сделает сама БД)
	// Но если бы не было CASCADE, здесь был бы DELETE FROM attachments WHERE note_id = $1

	// Удаляем заметку
	res, err := tx.Exec(`DELETE FROM notes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("ошибка при удалении заметки: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("ошибка при получении количества затронутых строк: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("заметка с ID %d не найдена для удаления", id)
	}

	// Если заметка успешно удалена из БД, удаляем физические файлы вложений
	for _, attach := range attachments {
		if err := os.Remove(attach.Filepath); err != nil {
			log.Printf("Ошибка при удалении файла вложения '%s': %v", attach.Filepath, err)
		} else {
			log.Printf("Файл вложения '%s' успешно удален с диска.", attach.Filepath)
		}
	}

	return tx.Commit()
}

// CreateAttachment создает запись о вложении в БД
func (s *PostgresStore) CreateAttachment(attachment *models.Attachment) error {
	query := `INSERT INTO attachments (note_id, filename, filepath, mimetype, size_bytes) VALUES ($1, $2, $3, $4, $5) RETURNING id, uploaded_at`
	err := s.db.QueryRow(query, attachment.NoteID, attachment.Filename, attachment.Filepath, attachment.MimeType, attachment.SizeBytes).Scan(&attachment.ID, &attachment.UploadedAt)
	if err != nil {
		return fmt.Errorf("ошибка при создании вложения: %w", err)
	}
	return nil
}

// GetAttachmentsByNoteID получает все вложения для указанной заметки
func (s *PostgresStore) GetAttachmentsByNoteID(noteID int) ([]models.Attachment, error) {
	query := `SELECT id, note_id, filename, filepath, mimetype, size_bytes, uploaded_at FROM attachments WHERE note_id = $1 ORDER BY uploaded_at ASC`
	rows, err := s.db.Query(query, noteID)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении вложений для заметки %d: %w", noteID, err)
	}
	defer rows.Close()

	var attachments []models.Attachment
	for rows.Next() {
		var attach models.Attachment
		if err := rows.Scan(&attach.ID, &attach.NoteID, &attach.Filename, &attach.Filepath, &attach.MimeType, &attach.SizeBytes, &attach.UploadedAt); err != nil {
			return nil, fmt.Errorf("ошибка при сканировании вложения: %w", err)
		}
		attachments = append(attachments, attach)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка после итерации по строкам вложений: %w", err)
	}
	return attachments, nil
}

// DeleteAttachment удаляет запись о вложении из БД и сам файл с диска
func (s *PostgresStore) DeleteAttachment(attachmentID int) error {
	// Сначала получаем путь к файлу
	var filepath string
	query := `SELECT filepath FROM attachments WHERE id = $1`
	err := s.db.QueryRow(query, attachmentID).Scan(&filepath)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("вложение с ID %d не найдено", attachmentID)
		}
		return fmt.Errorf("ошибка при получении пути к файлу вложения: %w", err)
	}

	// Удаляем запись из БД
	res, err := s.db.Exec(`DELETE FROM attachments WHERE id = $1`, attachmentID)
	if err != nil {
		return fmt.Errorf("ошибка при удалении вложения из БД: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("ошибка при проверке затронутых строк после удаления вложения: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("вложение с ID %d не найдено для удаления в БД", attachmentID)
	}

	// Удаляем физический файл
	if err := os.Remove(filepath); err != nil {
		// Логируем ошибку, но не возвращаем ее, так как запись из БД уже удалена
		log.Printf("Ошибка при удалении физического файла вложения '%s': %v", filepath, err)
	} else {
		log.Printf("Физический файл вложения '%s' успешно удален.", filepath)
	}

	return nil
}
