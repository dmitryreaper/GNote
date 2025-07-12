package main

import (
	"log"
	"os"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"GNote/storage"
	"GNote/ui"
)

func main() {

	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPortStr := os.Getenv("DB_PORT")
	dbPort, err := strconv.Atoi(dbPortStr)
	if err != nil {
		dbPort = 5432
	}
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "dima"
	}
	dbPassword := os.Getenv("DB_PASSWORD")
	if dbPassword == "" {
		dbPassword = ""
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "gnote_db"
	}
	dbSSLMode := os.Getenv("DB_SSLMODE")
	if dbSSLMode == "" {
		dbSSLMode = "disable"
	}

	dbConfig := storage.Config{
		Host:     dbHost,
		Port:     dbPort,
		User:     dbUser,
		Password: dbPassword,
		DBName:   dbName,
		SSLMode:  dbSSLMode,
	}

	// Инициализация хранилища (PostgreSQL)
	store, err := storage.NewPostgresStore(dbConfig)
	if err != nil {
		log.Fatalf("Ошибка при инициализации хранилища БД: %v", err)
	}

	a := app.New()
	w := a.NewWindow("Приложение для заметок")
	w.SetIcon(fyne.NewStaticResource("note.png", []byte{}))

	// Создание и запуск UI приложения
	noteApp := ui.NewNoteApp(w, store)
	_ = noteApp

	w.ShowAndRun()
}
