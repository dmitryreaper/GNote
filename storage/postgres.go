package storage

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
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

// CreateNote создает новую заметку в БД
func (s *PostgresStore) CreateNote(note *models.Note) error {
	query := `INSERT INTO notes (title, content) VALUES ($1, $2) RETURNING id, created_at, updated_at`
	err := s.db.QueryRow(query, note.Title, note.Content).Scan(&note.ID, &note.CreatedAt, &note.UpdatedAt)
	if err != nil {
		return fmt.Errorf("ошибка при создании заметки: %w", err)
	}
	return nil
}

// GetNoteByID получает заметку по ID
func (s *PostgresStore) GetNoteByID(id int) (*models.Note, error) {
	var note models.Note
	query := `SELECT id, title, content, created_at, updated_at FROM notes WHERE id = $1`
	err := s.db.QueryRow(query, id).Scan(&note.ID, &note.Title, &note.Content, &note.CreatedAt, &note.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("заметка с ID %d не найдена", id)
		}
		return nil, fmt.Errorf("ошибка при получении заметки по ID: %w", err)
	}
	return &note, nil
}

// GetAllNotes получает все заметки
func (s *PostgresStore) GetAllNotes() ([]models.Note, error) {
	rows, err := s.db.Query(`SELECT id, title, content, created_at, updated_at FROM notes ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении всех заметок: %w", err)
	}
	defer rows.Close()

	var notes []models.Note
	for rows.Next() {
		var note models.Note
		if err := rows.Scan(&note.ID, &note.Title, &note.Content, &note.CreatedAt, &note.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ошибка при сканировании заметки: %w", err)
		}
		notes = append(notes, note)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка после итерации по строкам: %w", err)
	}

	return notes, nil
}

// UpdateNote обновляет существующую заметку
func (s *PostgresStore) UpdateNote(note *models.Note) error {
	query := `UPDATE notes SET title = $1, content = $2, updated_at = $3 WHERE id = $4 RETURNING updated_at`
	// Принудительно обновляем updated_at, хотя триггер в БД тоже это сделает.
	// Это для того, чтобы получить актуальное значение сразу после запроса.
	note.UpdatedAt = time.Now()
	err := s.db.QueryRow(query, note.Title, note.Content, note.UpdatedAt, note.ID).Scan(&note.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("заметка с ID %d не найдена для обновления", note.ID)
		}
		// return nil, fmt.Errorf("ошибка при обновлении заметки: %w", err)
	}
	return nil
}

// DeleteNote удаляет заметку по ID
func (s *PostgresStore) DeleteNote(id int) error {
	res, err := s.db.Exec(`DELETE FROM notes WHERE id = $1`, id)
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
	return nil
}
