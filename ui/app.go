package ui

import (
	"fmt"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"GNote/models"
	"GNote/storage"
)

// NoteApp представляет собой основную структуру приложения Fyne
type NoteApp struct {
	window fyne.Window
	store  storage.Store

	notes      []models.Note 
	selectedID int           

	noteList    *widget.List
	titleEntry  *widget.Entry
	contentEntry *widget.Entry
	saveButton  *widget.Button
	deleteButton *widget.Button
}

// NewNoteApp создает новый экземпляр NoteApp
func NewNoteApp(w fyne.Window, s storage.Store) *NoteApp {
	app := &NoteApp{
		window: w,
		store:  s,
	}
	app.window.SetContent(app.MakeUI())
	app.window.SetMaster() 
	app.window.Resize(fyne.NewSize(800, 600))
	return app
}

// MakeUI создает и возвращает пользовательский интерфейс приложения
func (a *NoteApp) MakeUI() fyne.CanvasObject {
	// Левая панель: список заметок
	a.noteList = widget.NewList(
		func() int {
			return len(a.notes)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Название заметки")
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(a.notes[i].Title)
		},
	)
	a.noteList.OnSelected = a.onNoteSelected

	// Правая панель: детали заметки
	a.titleEntry = widget.NewEntry()
	a.titleEntry.SetPlaceHolder("Заголовок заметки")
	a.titleEntry.OnChanged = func(s string) {
		// Включаем кнопку сохранения при изменении
		a.saveButton.Enable()
	}

	a.contentEntry = widget.NewMultiLineEntry()
	a.contentEntry.SetPlaceHolder("Содержимое заметки...")
	a.contentEntry.Wrapping = fyne.TextWrapWord
	a.contentEntry.OnChanged = func(s string) {
		// Включаем кнопку сохранения при изменении
		a.saveButton.Enable()
	}

	a.saveButton = widget.NewButton("Сохранить", a.saveNote)
	a.saveButton.Disable() // Изначально отключена

	a.deleteButton = widget.NewButton("Удалить", a.deleteNote)
	a.deleteButton.Disable() // Изначально отключена

	newNoteButton := widget.NewButton("Новая заметка", a.newNote)

	// Контейнер для кнопок
	buttons := container.NewHBox(newNoteButton, a.saveButton, a.deleteButton)

	// Контейнер для деталей заметки
	noteDetailContainer := container.NewBorder(
		container.NewVBox(a.titleEntry, buttons), // Заголовок и кнопки сверху
		nil, // Ничего снизу
		nil, // Ничего слева
		nil, // Ничего справа
		container.NewScroll(a.contentEntry), // Содержимое с прокруткой в центре
	)

	// Горизонтальное разделение для списка и деталей
	split := container.NewHSplit(a.noteList, noteDetailContainer)
	split.SetOffset(0.25) // Список занимает 25% ширины

	// Загружаем заметки при старте
	a.loadNotes()
	a.newNote() // Начинаем с пустой формы для новой заметки

	return split
}

// loadNotes загружает заметки из БД и обновляет список
func (a *NoteApp) loadNotes() {
	notes, err := a.store.GetAllNotes()
	if err != nil {
		dialog.ShowError(fmt.Errorf("не удалось загрузить заметки: %w", err), a.window)
		log.Printf("Ошибка при загрузке заметок: %v", err)
		return
	}
	a.notes = notes
	a.noteList.Refresh()
	log.Println("Заметки загружены")
}

// onNoteSelected вызывается при выборе заметки из списка
func (a *NoteApp) onNoteSelected(id widget.ListItemID) {
	if id < 0 || id >= len(a.notes) {
		return // Некорректный ID
	}

	selectedNote := a.notes[id]
	a.selectedID = selectedNote.ID
	a.titleEntry.SetText(selectedNote.Title)
	a.contentEntry.SetText(selectedNote.Content)

	a.saveButton.Disable() // Отключаем, пока не будет изменений
	a.deleteButton.Enable()
	log.Printf("Выбрана заметка: %s (ID: %d)", selectedNote.Title, selectedNote.ID)
}

// newNote очищает поля для создания новой заметки
func (a *NoteApp) newNote() {
	a.selectedID = 0 
	a.titleEntry.SetText("")
	a.contentEntry.SetText("")
	a.saveButton.Disable() 
	a.deleteButton.Disable()
	a.noteList.UnselectAll() 
	log.Println("Подготовлена форма для новой заметки")
}

// saveNote сохраняет или обновляет заметку
func (a *NoteApp) saveNote() {
	title := a.titleEntry.Text
	content := a.contentEntry.Text

	if title == "" {
		dialog.ShowInformation("Ошибка", "Заголовок заметки не может быть пустым.", a.window)
		return
	}

	var err error
	if a.selectedID == 0 { // Новая заметка
		note := &models.Note{
			Title:   title,
			Content: content,
		}
		err = a.store.CreateNote(note)
		if err == nil {
			a.selectedID = note.ID 
			log.Printf("Создана новая заметка: %s (ID: %d)", note.Title, note.ID)
		}
	} else { // Обновление существующей
		note := &models.Note{
			ID:      a.selectedID,
			Title:   title,
			Content: content,
		}
		err = a.store.UpdateNote(note)
		if err == nil {
			log.Printf("Обновлена заметка: %s (ID: %d)", note.Title, note.ID)
		}
	}

	if err != nil {
		dialog.ShowError(fmt.Errorf("не удалось сохранить заметку: %w", err), a.window)
		log.Printf("Ошибка при сохранении заметки: %v", err)
		return
	}

	dialog.ShowInformation("Успех", "Заметка успешно сохранена!", a.window)
	a.saveButton.Disable() 
	a.deleteButton.Enable() 
	a.loadNotes()           
	// Попытка снова выбрать заметку после обновления списка
	for i, note := range a.notes {
		if note.ID == a.selectedID {
			a.noteList.Select(i)
			break
		}
	}
}

// deleteNote удаляет текущую заметку
func (a *NoteApp) deleteNote() {
	if a.selectedID == 0 {
		return // Ничего не выбрано для удаления
	}

	dialog.ShowConfirm("Подтверждение удаления",
		fmt.Sprintf("Вы уверены, что хотите удалить заметку '%s'?", a.titleEntry.Text),
		func(confirmed bool) {
			if confirmed {
				err := a.store.DeleteNote(a.selectedID)
				if err != nil {
					dialog.ShowError(fmt.Errorf("не удалось удалить заметку: %w", err), a.window)
					log.Printf("Ошибка при удалении заметки: %v", err)
					return
				}
				dialog.ShowInformation("Успех", "Заметка успешно удалена.", a.window)
				log.Printf("Удалена заметка с ID: %d", a.selectedID)
				a.loadNotes() // Перезагружаем список
				a.newNote()   // Переходим к созданию новой заметки
			}
		}, a.window)
}
