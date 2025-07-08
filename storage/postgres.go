package storage

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	pq "github.com/lib/pq" 
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

// GetNoteByID получает заметку по ID, включая теги
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

	return &note, nil
}

// GetAllNotes получает все заметки, включая теги
func (s *PostgresStore) GetAllNotes() ([]models.Note, error) {
	// Используем LEFT JOIN для получения всех заметок и их тегов
	// ARRAY_AGG в Postgres для объединения тегов в одну строку
	// COALESCE(ARRAY_AGG(...), '{}') для обработки заметок без тегов (возвращает пустой массив вместо NULL)
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
		var tagsArray []string // Изменено: теперь сканируем прямо в []string
		var reminderAtSQL sql.NullTime

		// Изменено: используем pq.Array для сканирования массива тегов
		if err := rows.Scan(&note.ID, &note.Title, &note.Content, &note.CreatedAt, &note.UpdatedAt, &reminderAtSQL, pq.Array(&tagsArray)); err != nil {
			return nil, fmt.Errorf("ошибка при сканировании заметки: %w", err)
		}

		if reminderAtSQL.Valid {
			note.ReminderAt = &reminderAtSQL.Time
		}

		// Теперь tagsArray уже содержит []string, не нужно дополнительно преобразовывать
		note.Tags = tagsArray
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

	// Удаляем привязки тегов (CASCADE в БД позаботится об этом, но можно явно)
	_, err = tx.Exec(`DELETE FROM note_tags WHERE note_id = $1`, id)
	if err != nil {
		return fmt.Errorf("ошибка при удалении привязок тегов: %w", err)
	}

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

	return tx.Commit()
}
