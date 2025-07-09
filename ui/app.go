package ui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io/ioutil"
	"log"
	"sort"
	"strings"
	"time"
	"os"     
	"path/filepath"
	"mime"
	"os/exec"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"GNote/models"
	"GNote/storage"
)

// NoteApp представляет собой основную структуру приложения Fyne
type NoteApp struct {
	window fyne.Window
	store  storage.Store

	allNotes          []models.Note // Все загруженные заметки
	filteredNotes     []models.Note // Отфильтрованные заметки для отображения в списке
	selectedNoteIndex int           // Индекс выбранной заметки в filteredNotes (-1, если ничего не выбрано)
	hasUnsavedChanges bool          // Флаг для отслеживания несохраненных изменений

	// UI элементы
	noteList       *widget.List
	searchEntry    *widget.Entry
	sortSelect     *widget.Select
	titleEntry     *widget.Entry
	contentEntry   *widget.Entry
	charCountLabel *widget.Label
	tagsEntry      *widget.Entry 
	reminderButton *widget.Button 
	reminderLabel  *widget.Label  
	saveButton     *widget.Button
	deleteButton   *widget.Button

	// Для диалога напоминания
	reminderDateEntry *widget.Entry
	reminderTimeEntry *widget.Entry
	currentReminder   *time.Time // Временное хранилище для даты/времени напоминания в диалоге

	// НОВЫЕ ЭЛЕМЕНТЫ ДЛЯ ВЛОЖЕНИЙ
	attachmentsContainer *fyne.Container // Контейнер для списка вложений и кнопки "Прикрепить"
	attachmentsList      *widget.List    // Список отображаемых вложений
	attachButton         *widget.Button  // Кнопка для прикрепления файла
	attachmentsDirPath   string          // Путь к директории для хранения вложений
}

// NewNoteApp создает новый экземпляр NoteApp
func NewNoteApp(w fyne.Window, s storage.Store) *NoteApp {
	app := &NoteApp{
		window:            w,
		store:             s,
		selectedNoteIndex: -1, 
		hasUnsavedChanges: false,
	}
	app.window.SetContent(app.MakeUI())
	app.window.SetMaster() // Устанавливаем окно как основное
	app.window.Resize(fyne.NewSize(1000, 700)) // Устанавливаем начальный размер
	app.window.SetOnClosed(app.onWindowClosed) // Обработчик закрытия окна

	// Определяем путь для хранения вложений
	// Используем Storage().RootURI().Path() для кроссплатформенного пути к данным приложения
	appDataPath := fyne.CurrentApp().Storage().RootURI().Path()
	app.attachmentsDirPath = filepath.Join(appDataPath, "attachments")
	// Создаем директорию, если она не существует
	if err := os.MkdirAll(app.attachmentsDirPath, 0755); err != nil {
		log.Printf("Ошибка при создании директории для вложений '%s': %v", app.attachmentsDirPath, err)
		dialog.ShowError(fmt.Errorf("не удалось создать директорию для вложений: %w", err), app.window)
	} else {
		log.Printf("Директория для вложений: %s", app.attachmentsDirPath)
	}

	// Загружаем заметки при старте
	app.loadNotes()
	app.newNote() // Начинаем с пустой формы для новой заметки
	return app
}

// MakeUI создает и возвращает пользовательский интерфейс приложения
func (a *NoteApp) MakeUI() fyne.CanvasObject {
	// --- Левая панель: Поиск, Сортировка, Список заметок ---
	a.searchEntry = widget.NewEntry()
	a.searchEntry.SetPlaceHolder("Поиск по заголовку, содержимому или тегам...")
	a.searchEntry.OnChanged = func(s string) {
		a.filterNotes()
	}

	// Инициализируем a.noteList ДО a.sortSelect
	a.noteList = widget.NewList(
		func() int {
			return len(a.filteredNotes)
		},
		func() fyne.CanvasObject {
			// Кастомный элемент списка для выделения фона
			bg := canvas.NewRectangle(color.Transparent) // Фон
			label := widget.NewLabel("Название заметки") // Текст
			return container.NewMax(bg, label)           // bg будет под label
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			note := a.filteredNotes[i]
			box := o.(*fyne.Container)
			bg := box.Objects[0].(*canvas.Rectangle)
			label := box.Objects[1].(*widget.Label)

			label.SetText(note.Title)

			// Визуальное выделение активной заметки
			if i == a.selectedNoteIndex {
				bg.FillColor = theme.PrimaryColor() // Используем PrimaryColor для фона
				label.TextStyle.Bold = true
			} else {
				bg.FillColor = color.Transparent // Прозрачный фон
				label.TextStyle.Bold = false
			}
			bg.Refresh()
			label.Refresh()
		},
	)
	a.noteList.OnSelected = a.onNoteSelected
	a.noteList.OnUnselected = func(id widget.ListItemID) {
		// При сбросе выделения, убедимся, что стиль сброшен
		// Это важно, так как Fyne переиспользует объекты списка
		if id >= 0 && id < len(a.filteredNotes) {
			// Вызываем UpdateItem для сброса стиля
			a.noteList.UpdateItem(id, a.noteList.CreateItem())
		}
	}

	a.sortSelect = widget.NewSelect([]string{
		"По дате создания (новые)",
		"По дате создания (старые)",
		"По дате обновления (новые)",
		"По дате обновления (старые)",
		"По заголовку (А-Я)",
		"По заголовку (Я-А)",
	}, func(s string) {
		a.sortNotes(s)
		a.noteList.Refresh() // Теперь a.noteList инициализирован
	})
	a.sortSelect.SetSelectedIndex(0) // Это вызовет коллбэк OnChanged

	leftPanel := container.NewBorder(
		container.NewVBox(a.searchEntry, a.sortSelect), // Поиск и сортировка сверху
		nil,
		nil,
		nil,
		a.noteList,
	)

	// --- Правая панель: Детали заметки и кнопки ---
	a.titleEntry = widget.NewEntry()
	a.titleEntry.SetPlaceHolder("Заголовок заметки")
	a.titleEntry.OnChanged = func(s string) {
		a.setUnsavedChanges(true)
	}

	a.contentEntry = widget.NewMultiLineEntry()
	a.contentEntry.SetPlaceHolder("Содержимое заметки...")
	a.contentEntry.Wrapping = fyne.TextWrapWord
	a.contentEntry.OnChanged = func(s string) {
		a.setUnsavedChanges(true)
		a.updateCharCount()
	}

	a.charCountLabel = widget.NewLabel("Символов: 0 | Слов: 0")
	a.charCountLabel.Alignment = fyne.TextAlignTrailing // Выравнивание по правому краю

	a.tagsEntry = widget.NewEntry()
	a.tagsEntry.SetPlaceHolder("Теги (через запятую, например: работа, личное)")
	a.tagsEntry.OnChanged = func(s string) {
		a.setUnsavedChanges(true)
	}

	a.reminderLabel = widget.NewLabel("Напоминание: Не установлено")
	a.reminderButton = widget.NewButton("Установить напоминание", a.setReminderDialog)
	clearReminderButton := widget.NewButton("Очистить", func() {
		a.setUnsavedChanges(true)
		a.updateReminderUI(nil)
	})
	reminderContainer := container.NewHBox(a.reminderLabel, a.reminderButton, clearReminderButton)

	// НОВЫЙ БЛОК: Вложения
	a.attachButton = widget.NewButtonWithIcon("Прикрепить файл", theme.ContentAddIcon(), a.attachFile)
	a.attachButton.Disable() // Изначально отключена, пока не выбрана заметка

	a.attachmentsList = widget.NewList(
		func() int {
			selectedNote := a.getSelectedNote()
			if selectedNote == nil {
				return 0
			}
			return len(selectedNote.Attachments)
		},
		func() fyne.CanvasObject {
			// Кастомный элемент списка для вложений
			filenameLabel := widget.NewLabel("Имя файла")
			sizeLabel := widget.NewLabel("Размер")
			openButton := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), nil)
			deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)
			return container.NewHBox(filenameLabel, layout.NewSpacer(), sizeLabel, openButton, deleteButton)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			selectedNote := a.getSelectedNote()
			if selectedNote == nil || i >= len(selectedNote.Attachments) {
				return
			}
			attachment := selectedNote.Attachments[i]

			hbox := o.(*fyne.Container)
			filenameLabel := hbox.Objects[0].(*widget.Label)
			sizeLabel := hbox.Objects[2].(*widget.Label)
			openButton := hbox.Objects[3].(*widget.Button)
			deleteButton := hbox.Objects[4].(*widget.Button)

			filenameLabel.SetText(attachment.Filename)
			sizeLabel.SetText(formatBytes(attachment.SizeBytes))

			// Обработчики кнопок для каждого элемента списка
			openButton.OnTapped = func() {
				a.openAttachment(attachment)
			}
			deleteButton.OnTapped = func() {
				a.deleteAttachment(attachment)
			}
		},
	)
	a.attachmentsContainer = container.NewBorder(
		container.NewHBox(widget.NewLabel("Вложения:"), layout.NewSpacer(), a.attachButton),
		nil,
		nil,
		nil,
		container.NewScroll(a.attachmentsList),
	)
	// КОНЕЦ НОВОГО БЛОКА ВЛОЖЕНИЙ

	a.saveButton = widget.NewButtonWithIcon("Сохранить", theme.DocumentSaveIcon(), a.saveNote)
	a.saveButton.Disable()

	a.deleteButton = widget.NewButtonWithIcon("Удалить", theme.DeleteIcon(), a.deleteNote)
	a.deleteButton.Disable()

	newNoteButton := widget.NewButtonWithIcon("Новая заметка", theme.ContentAddIcon(), a.newNote)
	exportButton := widget.NewButtonWithIcon("Экспорт", theme.DownloadIcon(), a.exportNote)
	importButton := widget.NewButtonWithIcon("Импорт", theme.UploadIcon(), a.importNote)
	aboutButton := widget.NewButtonWithIcon("О программе", theme.InfoIcon(), a.showAboutDialog)

	// Контейнер для кнопок действий
	actionButtons := container.New(layout.NewGridLayoutWithColumns(4),
		newNoteButton, a.saveButton, a.deleteButton, exportButton,
		importButton, aboutButton,
	)

	// Контейнер для деталей заметки
	noteDetailContainer := container.NewBorder(
		container.NewVBox(
			a.titleEntry,
			a.tagsEntry,
			reminderContainer,
			widget.NewSeparator(),
			a.attachmentsContainer, // <-- ДОБАВЛЕНО: Контейнер для вложений
			widget.NewSeparator(),
		), // Заголовок, теги, напоминание, вложения сверху
		container.NewVBox(
			a.charCountLabel,
			actionButtons,
		), // Счетчик символов и кнопки снизу
		nil,
		nil,
		container.NewScroll(a.contentEntry), // Содержимое с прокруткой в центре
	)

	// Горизонтальное разделение для списка и деталей
	split := container.NewHSplit(leftPanel, noteDetailContainer)
	split.SetOffset(0.25) // Список занимает 25% ширины

	return split
}

// setUnsavedChanges устанавливает флаг несохраненных изменений и обновляет состояние кнопки "Сохранить"
func (a *NoteApp) setUnsavedChanges(changed bool) {
	a.hasUnsavedChanges = changed
	if changed {
		a.saveButton.Enable()
	} else {
		a.saveButton.Disable()
	}
}

// loadNotes загружает заметки из БД, фильтрует и сортирует их
func (a *NoteApp) loadNotes() {
	notes, err := a.store.GetAllNotes()
	if err != nil {
		dialog.ShowError(fmt.Errorf("не удалось загрузить заметки: %w", err), a.window)
		log.Printf("Ошибка при загрузке заметок: %v", err)
		return
	}
	a.allNotes = notes
	a.filterNotes()             // Применяем текущий фильтр
	a.sortNotes(a.sortSelect.Selected) // Применяем текущую сортировку
	a.noteList.Refresh()
	log.Println("Заметки загружены и отфильтрованы/отсортированы")
}

// filterNotes фильтрует заметки на основе поискового запроса
func (a *NoteApp) filterNotes() {
	query := strings.ToLower(a.searchEntry.Text)
	if query == "" {
		a.filteredNotes = a.allNotes
	} else {
		a.filteredNotes = []models.Note{}
		for _, note := range a.allNotes {
			if strings.Contains(strings.ToLower(note.Title), query) ||
				strings.Contains(strings.ToLower(note.Content), query) ||
				strings.Contains(strings.ToLower(strings.Join(note.Tags, ",")), query) { // Поиск по тегам
				a.filteredNotes = append(a.filteredNotes, note)
			}
		}
	}
	a.sortNotes(a.sortSelect.Selected) // Пересортируем после фильтрации
	a.noteList.Refresh()
	// Если выбранная заметка больше не в отфильтрованном списке, сбросить выбор
	if a.selectedNoteIndex != -1 {
		selectedNote := a.getSelectedNote() // Получаем текущую выбранную заметку
		found := false
		for i, note := range a.filteredNotes {
			if selectedNote != nil && note.ID == selectedNote.ID {
				a.selectedNoteIndex = i // Обновляем индекс, если заметка все еще в списке
				a.noteList.Select(i)
				found = true
				break
			}
		}
		if !found {
			a.noteList.UnselectAll()
			a.selectedNoteIndex = -1
			a.newNote() // Очищаем поля, если выбранная заметка пропала
		}
	}
}

// sortNotes сортирует filteredNotes на основе выбранного критерия
func (a *NoteApp) sortNotes(criteria string) {
	switch criteria {
	case "По дате создания (новые)":
		sort.Slice(a.filteredNotes, func(i, j int) bool {
			return a.filteredNotes[i].CreatedAt.After(a.filteredNotes[j].CreatedAt)
		})
	case "По дате создания (старые)":
		sort.Slice(a.filteredNotes, func(i, j int) bool {
			return a.filteredNotes[i].CreatedAt.Before(a.filteredNotes[j].CreatedAt)
		})
	case "По дате обновления (новые)":
		sort.Slice(a.filteredNotes, func(i, j int) bool {
			return a.filteredNotes[i].UpdatedAt.After(a.filteredNotes[j].UpdatedAt)
		})
	case "По дате обновления (старые)":
		sort.Slice(a.filteredNotes, func(i, j int) bool {
			return a.filteredNotes[i].UpdatedAt.Before(a.filteredNotes[j].UpdatedAt)
		})
	case "По заголовку (А-Я)":
		sort.Slice(a.filteredNotes, func(i, j int) bool {
			return strings.ToLower(a.filteredNotes[i].Title) < strings.ToLower(a.filteredNotes[j].Title)
		})
	case "По заголовку (Я-А)":
		sort.Slice(a.filteredNotes, func(i, j int) bool {
			return strings.ToLower(a.filteredNotes[i].Title) > strings.ToLower(a.filteredNotes[j].Title)
		})
	}
}

// getSelectedNote возвращает выбранную заметку или nil
func (a *NoteApp) getSelectedNote() *models.Note {
	if a.selectedNoteIndex == -1 || a.selectedNoteIndex >= len(a.filteredNotes) {
		return nil
	}
	return &a.filteredNotes[a.selectedNoteIndex]
}

// onNoteSelected вызывается при выборе заметки из списка
func (a *NoteApp) onNoteSelected(id widget.ListItemID) {
	if a.hasUnsavedChanges {
		a.showUnsavedChangesDialog(func() {
			a.doSelectNote(id)
		})
	} else {
		a.doSelectNote(id)
	}
}

// doSelectNote выполняет фактический выбор заметки после проверки изменений
func (a *NoteApp) doSelectNote(id widget.ListItemID) {
	if id < 0 || id >= len(a.filteredNotes) {
		return // Некорректный ID
	}

	// Загружаем заметку с вложениями из БД
	selectedNoteFromDB, err := a.store.GetNoteByID(a.filteredNotes[id].ID)
	if err != nil {
		dialog.ShowError(fmt.Errorf("не удалось загрузить детали заметки: %w", err), a.window)
		log.Printf("Ошибка при загрузке деталей заметки: %v", err)
		return
	}

	// Обновляем заметку в filteredNotes, чтобы она содержала вложения
	a.filteredNotes[id] = *selectedNoteFromDB
	a.selectedNoteIndex = id
	selectedNote := a.filteredNotes[id] // Используем обновленную заметку

	a.titleEntry.SetText(selectedNote.Title)
	a.contentEntry.SetText(selectedNote.Content)
	a.tagsEntry.SetText(strings.Join(selectedNote.Tags, ", "))
	a.updateReminderUI(selectedNote.ReminderAt)

	a.setUnsavedChanges(false) // Сброс флага после загрузки
	a.deleteButton.Enable()
	a.attachButton.Enable() // Включаем кнопку "Прикрепить файл"
	a.updateCharCount()     // Обновить счетчик для выбранной заметки
	a.attachmentsList.Refresh() // Обновляем список вложений
	log.Printf("Выбрана заметка: %s (ID: %d)", selectedNote.Title, selectedNote.ID)

	// Обновляем визуальное выделение
	a.noteList.Refresh()
}

// newNote очищает поля для создания новой заметки
func (a *NoteApp) newNote() {
	if a.hasUnsavedChanges {
		a.showUnsavedChangesDialog(func() {
			a.doNewNote()
		})
	} else {
		a.doNewNote()
	}
}

// doNewNote выполняет фактическое создание новой заметки после проверки изменений
func (a *NoteApp) doNewNote() {
	a.selectedNoteIndex = -1 // Указываем, что это новая заметка
	a.titleEntry.SetText("")
	a.contentEntry.SetText("")
	a.tagsEntry.SetText("")
	a.updateReminderUI(nil) // Сброс напоминания
	a.setUnsavedChanges(false)
	a.deleteButton.Disable()
	a.attachButton.Disable() // Отключаем кнопку "Прикрепить файл" для новой заметки (пока не сохранена)
	a.noteList.UnselectAll() // Снимаем выделение со списка
	a.updateCharCount()      // Обновить счетчик для пустой заметки
	// Очищаем список вложений для новой/несвязанной заметки
	if a.attachmentsList != nil {
		a.attachmentsList.Refresh()
	}
	log.Println("Подготовлена форма для новой заметки")
	a.noteList.Refresh() // Обновляем список, чтобы снять выделение
}

// saveNote сохраняет или обновляет заметку
func (a *NoteApp) saveNote() {
	title := a.titleEntry.Text
	content := a.contentEntry.Text
	tags := parseTags(a.tagsEntry.Text)
	var reminderAt *time.Time
	// Проверяем, установлено ли напоминание, и пытаемся его распарсить
	if a.reminderLabel.Text != "Напоминание: Не установлено" {
		// Формат, используемый в updateReminderUI
		t, err := time.Parse("Напоминание: 02.01.2006 15:04", a.reminderLabel.Text)
		if err == nil {
			reminderAt = &t
		} else {
			log.Printf("Ошибка парсинга напоминания из UI: %v", err)
		}
	}

	if title == "" {
		dialog.ShowInformation("Ошибка", "Заголовок заметки не может быть пустым.", a.window)
		return
	}

	var err error
	var currentNote *models.Note
	if a.getSelectedNote() == nil { // Новая заметка
		note := &models.Note{
			Title:      title,
			Content:    content,
			Tags:       tags,
			ReminderAt: reminderAt,
		}
		err = a.store.CreateNote(note)
		currentNote = note
		if err == nil {
			log.Printf("Создана новая заметка: %s (ID: %d)", note.Title, note.ID)
		}
	} else { // Обновление существующей
		note := a.getSelectedNote()
		note.Title = title
		note.Content = content
		note.Tags = tags
		note.ReminderAt = reminderAt
		err = a.store.UpdateNote(note)
		currentNote = note
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
	a.setUnsavedChanges(false) // Сброс флага после сохранения
	a.deleteButton.Enable()
	a.attachButton.Enable() // Включаем кнопку "Прикрепить файл" после сохранения
	a.loadNotes()           // Перезагружаем список, чтобы обновить/добавить заметку
	// Попытка снова выбрать заметку после обновления списка
	if currentNote != nil {
		for i, note := range a.filteredNotes {
			if note.ID == currentNote.ID {
				a.noteList.Select(i)
				// Убедимся, что selectedNoteIndex обновлен корректно
				a.selectedNoteIndex = i
				// Перезагружаем вложения для выбранной заметки после сохранения
				a.doSelectNote(i) // Это обновит вложения
				break
			}
		}
	}
}

// deleteNote удаляет текущую заметку
func (a *NoteApp) deleteNote() {
	selectedNote := a.getSelectedNote()
	if selectedNote == nil {
		return // Ничего не выбрано для удаления
	}

	dialog.ShowConfirm("Подтверждение удаления",
		fmt.Sprintf("Вы уверены, что хотите удалить заметку '%s'? Все связанные вложения также будут удалены.", selectedNote.Title),
		func(confirmed bool) {
			if confirmed {
				err := a.store.DeleteNote(selectedNote.ID)
				if err != nil {
					dialog.ShowError(fmt.Errorf("не удалось удалить заметку: %w", err), a.window)
					log.Printf("Ошибка при удалении заметки: %v", err)
					return
				}
				dialog.ShowInformation("Успех", "Заметка успешно удалена.", a.window)
				log.Printf("Удалена заметка с ID: %d", selectedNote.ID)
				a.loadNotes() // Перезагружаем список
				a.newNote()   // Переходим к созданию новой заметки
			}
		}, a.window)
}

// updateCharCount обновляет счетчик символов и слов
func (a *NoteApp) updateCharCount() {
	content := a.contentEntry.Text
	chars := len(content)
	words := len(strings.Fields(content)) // Разделяем по пробелам и считаем
	a.charCountLabel.SetText(fmt.Sprintf("Символов: %d | Слов: %d", chars, words))
}

// showUnsavedChangesDialog показывает диалог подтверждения несохраненных изменений
func (a *NoteApp) showUnsavedChangesDialog(onContinue func()) {
	dialog.ShowConfirm("Несохраненные изменения",
		"У вас есть несохраненные изменения. Сохранить их?",
		func(save bool) {
			if save {
				a.saveNote() // Попытаться сохранить
				// Если сохранение успешно, тогда продолжить
				// Мы не можем гарантировать, что saveNote() завершится до того, как этот коллбэк вернется.
				// Лучше вызвать onContinue() после успешного сохранения внутри saveNote(),
				// или просто продолжить, если пользователь выбрал "Сохранить"
				// Для простоты сейчас, предполагаем, что если пользователь выбрал сохранить,
				// он хочет продолжить, даже если сохранение не удалось (появится ошибка).
				// В более сложном приложении здесь нужна более сложная логика.
				onContinue()
			} else {
				a.setUnsavedChanges(false) // Отменить изменения
				onContinue()
			}
		}, a.window)
}

// onWindowClosed обрабатывает закрытие окна
func (a *NoteApp) onWindowClosed() {
	if a.hasUnsavedChanges {
		a.showUnsavedChangesDialog(func() {
			// Если пользователь выбрал не сохранять или сохранил,
			// то закрываем приложение
			if !a.hasUnsavedChanges { // Если флаг сброшен, значит, сохранение прошло успешно или отменено
				a.window.Close()
			}
		})
	} else {
		a.window.Close()
	}
}

// parseTags преобразует строку тегов в срез строк
func parseTags(tagString string) []string {
	tags := strings.Split(tagString, ",")
	cleanTags := []string{}
	for _, tag := range tags {
		trimmedTag := strings.TrimSpace(tag)
		if trimmedTag != "" {
			cleanTags = append(cleanTags, trimmedTag)
		}
	}
	return cleanTags
}

// updateReminderUI обновляет отображение напоминания
func (a *NoteApp) updateReminderUI(t *time.Time) {
	if t == nil {
		a.reminderLabel.SetText("Напоминание: Не установлено")
		a.currentReminder = nil
	} else {
		a.reminderLabel.SetText(fmt.Sprintf("Напоминание: %s", t.Format("02.01.2006 15:04")))
		a.currentReminder = t
	}
}

// setReminderDialog открывает диалог для установки напоминания
func (a *NoteApp) setReminderDialog() {
	// Инициализируем текущее напоминание для диалога
	initialTime := time.Now()
	if a.currentReminder != nil {
		initialTime = *a.currentReminder
	}

	a.reminderDateEntry = widget.NewEntry()
	a.reminderDateEntry.SetPlaceHolder("ДД.ММ.ГГГГ")
	a.reminderDateEntry.SetText(initialTime.Format("02.01.2006"))

	a.reminderTimeEntry = widget.NewEntry()
	a.reminderTimeEntry.SetPlaceHolder("ЧЧ:ММ")
	a.reminderTimeEntry.SetText(initialTime.Format("15:04"))

	// Кнопка для открытия календаря
	calendarButton := widget.NewButton("Выбрать дату", func() {
		dialog.ShowCustom("Выберите дату", "Закрыть",
			widget.NewCalendar(initialTime, func(t time.Time) {
				a.reminderDateEntry.SetText(t.Format("02.01.2006"))
			}), a.window)
	})

	content := container.NewVBox(
		widget.NewLabel("Дата:"),
		container.NewHBox(a.reminderDateEntry, calendarButton),
		widget.NewLabel("Время (ЧЧ:ММ):"),
		a.reminderTimeEntry,
	)

	dialog.ShowCustomConfirm("Установить напоминание", "Установить", "Отмена", content, func(ok bool) {
		if ok {
			dateStr := a.reminderDateEntry.Text
			timeStr := a.reminderTimeEntry.Text
			combinedStr := fmt.Sprintf("%s %s", dateStr, timeStr)

			parsedTime, err := time.Parse("02.01.2006 15:04", combinedStr)
			if err != nil {
				dialog.ShowError(fmt.Errorf("неверный формат даты или времени. Используйте ДД.ММ.ГГГГ ЧЧ:ММ: %w", err), a.window)
				return
			}
			a.updateReminderUI(&parsedTime)
			a.setUnsavedChanges(true)
		}
	}, a.window)
}

// exportNote экспортирует выбранную заметку или все заметки
func (a *NoteApp) exportNote() {
	dialog.ShowConfirm("Экспорт заметок",
		"Экспортировать только текущую заметку или все заметки?",
		func(exportAll bool) {
			var notesToExport []models.Note
			if exportAll {
				notesToExport = a.allNotes // Экспортируем все заметки
				// Для экспорта всех заметок, нужно загрузить их вложения
				for i, note := range notesToExport {
					attachments, err := a.store.GetAttachmentsByNoteID(note.ID)
					if err != nil {
						log.Printf("Ошибка при загрузке вложений для заметки ID %d при экспорте: %v", note.ID, err)
						// Продолжаем, но без вложений для этой заметки
					}
					notesToExport[i].Attachments = attachments
				}
			} else {
				selectedNote := a.getSelectedNote()
				if selectedNote == nil {
					dialog.ShowInformation("Ошибка", "Для экспорта текущей заметки, пожалуйста, выберите заметку.", a.window)
					return
				}
				notesToExport = []models.Note{*selectedNote}
			}

			dialog.ShowFileSave(func(writer fyne.URIWriteCloser, err error) {
				if err != nil {
					dialog.ShowError(err, a.window)
					return
				}
				if writer == nil { // Пользователь отменил
					return
				}
				defer writer.Close()

				// Простой формат JSON для экспорта
				data, err := json.MarshalIndent(notesToExport, "", "  ")
				if err != nil {
					dialog.ShowError(fmt.Errorf("ошибка при форматировании JSON: %w", err), a.window)
					return
				}

				_, err = writer.Write(data)
				if err != nil {
					dialog.ShowError(fmt.Errorf("ошибка при записи файла: %w", err), a.window)
					return
				}
				dialog.ShowInformation("Экспорт", "Заметки успешно экспортированы!", a.window)
			}, a.window)
		}, a.window)
}

// importNote импортирует заметки из файла JSON
func (a *NoteApp) importNote() {
	dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if reader == nil { // Пользователь отменил
			return
		}
		defer reader.Close()

		data, err := ioutil.ReadAll(reader)
		if err != nil {
			dialog.ShowError(fmt.Errorf("ошибка при чтении файла: %w", err), a.window)
			return
		}

		var importedNotes []models.Note
		err = json.Unmarshal(data, &importedNotes)
		if err != nil {
			dialog.ShowError(fmt.Errorf("ошибка при парсинге JSON: %w", err), a.window)
			return
		}

		if len(importedNotes) == 0 {
			dialog.ShowInformation("Импорт", "В файле не найдено заметок для импорта.", a.window)
			return
		}

		dialog.ShowConfirm("Импорт заметок",
			fmt.Sprintf("Вы уверены, что хотите импортировать %d заметки(ок)? Существующие заметки с такими же ID будут перезаписаны, а новые добавлены. Вложения будут импортированы, если файлы существуют.", len(importedNotes)),
			func(confirmed bool) {
				if !confirmed {
					return
				}

				importedCount := 0
				for _, note := range importedNotes {
					// Попытаемся обновить, если заметка с таким ID уже существует
					existingNote, getErr := a.store.GetNoteByID(note.ID)
					if getErr == nil && existingNote != nil {
						// Заметка существует, обновляем
						// Сохраняем оригинальные даты создания/обновления из БД, если они не заданы в импортированной заметке
						if note.CreatedAt.IsZero() {
							note.CreatedAt = existingNote.CreatedAt
						}
						// Fyne DatePicker/TimePicker не возвращают часовой пояс, поэтому убедимся, что время в UTC, если это важно
						if note.ReminderAt != nil && note.ReminderAt.Location().String() == "Local" {
							utcTime := note.ReminderAt.In(time.UTC)
							note.ReminderAt = &utcTime
						}

						if err := a.store.UpdateNote(&note); err != nil {
							log.Printf("Ошибка при обновлении заметки ID %d: %v", note.ID, err)
							continue
						}
					} else {
						// Заметка не существует или ошибка при получении, создаем новую
						// Обнуляем ID, чтобы БД сгенерировала новый
						note.ID = 0
						// Fyne DatePicker/TimePicker не возвращают часовой пояс, поэтому убедимся, что время в UTC, если это важно
						if note.ReminderAt != nil && note.ReminderAt.Location().String() == "Local" {
							utcTime := note.ReminderAt.In(time.UTC)
							note.ReminderAt = &utcTime
						}
						if err := a.store.CreateNote(&note); err != nil {
							log.Printf("Ошибка при создании заметки '%s': %v", note.Title, err)
							continue
						}
					}
					importedCount++

					// Импортируем вложения для этой заметки
					for _, attach := range note.Attachments {
						// Здесь мы предполагаем, что файлы вложений должны быть скопированы вручную
						// или быть доступны по исходным путям.
						// Для реального импорта, нужно будет скопировать файлы и обновить filepath.
						// Сейчас просто создаем запись в БД, если файл существует по указанному пути.
						if _, err := os.Stat(attach.Filepath); err == nil {
							// Файл существует, создаем запись в БД
							attach.NoteID = note.ID // Привязываем к только что созданной/обновленной заметке
							if err := a.store.CreateAttachment(&attach); err != nil {
								log.Printf("Ошибка при импорте вложения '%s' для заметки ID %d: %v", attach.Filename, note.ID, err)
							}
						} else {
							log.Printf("Файл вложения '%s' не найден по пути '%s', запись не импортирована.", attach.Filename, attach.Filepath)
						}
					}
				}

				if importedCount > 0 {
					dialog.ShowInformation("Импорт", fmt.Sprintf("Успешно импортировано %d заметок.", importedCount), a.window)
					a.loadNotes() // Перезагружаем список после импорта
					a.newNote()
				} else {
					dialog.ShowError(fmt.Errorf("не удалось импортировать ни одной заметки"), a.window)
				}
			}, a.window)
	}, a.window)
}

// showAboutDialog показывает окно "О программе"
func (a *NoteApp) showAboutDialog() {
	content := container.NewVBox(
		widget.NewLabel("Приложение для заметок"),
		widget.NewLabel("Версия: 1.0"),
		widget.NewLabel("Автор: [Ваше Имя/Название]"),
		widget.NewLabel("Год: 2025"),
		widget.NewLabel(""),
		widget.NewLabel("Это простое приложение для ведения заметок с использованием Go и Fyne."),
		widget.NewLabel("Данные хранятся в PostgreSQL."),
	)
	dialog.ShowCustom("О программе", "Закрыть", content, a.window)
}

// НОВЫЕ ФУНКЦИИ ДЛЯ ВЛОЖЕНИЙ

// attachFile открывает диалог выбора файла и прикрепляет его к текущей заметке
func (a *NoteApp) attachFile() {
	selectedNote := a.getSelectedNote()
	if selectedNote == nil {
		dialog.ShowInformation("Ошибка", "Сначала выберите или сохраните заметку, чтобы прикрепить к ней файл.", a.window)
		return
	}

	dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if reader == nil { // Пользователь отменил выбор
			return
		}
		defer reader.Close()

		originalFilename := filepath.Base(reader.URI().Path())
		// Генерируем уникальное имя файла для хранения, чтобы избежать коллизий
		uniqueFilename := fmt.Sprintf("%d_%s_%s", selectedNote.ID, time.Now().Format("20060102150405"), originalFilename)
		destPath := filepath.Join(a.attachmentsDirPath, uniqueFilename)

		// Копируем файл
		destFile, err := os.Create(destPath)
		if err != nil {
			dialog.ShowError(fmt.Errorf("не удалось создать файл вложения: %w", err), a.window)
			return
		}
		defer destFile.Close()

		fileContent, err := ioutil.ReadAll(reader)
		if err != nil {
			dialog.ShowError(fmt.Errorf("не удалось прочитать файл: %w", err), a.window)
			return
		}
		_, err = destFile.Write(fileContent)
		if err != nil {
			dialog.ShowError(fmt.Errorf("не удалось записать файл: %w", err), a.window)
			return
		}

		// Получаем MIME-тип
		mimeType := mime.TypeByExtension(filepath.Ext(originalFilename))
		if mimeType == "" {
			mimeType = "application/octet-stream" // Дефолтный тип, если не удалось определить
		}

		// Создаем запись в БД
		attachment := &models.Attachment{
			NoteID:    selectedNote.ID,
			Filename:  originalFilename,
			Filepath:  destPath,
			MimeType:  mimeType,
			SizeBytes: int64(len(fileContent)),
		}

		err = a.store.CreateAttachment(attachment)
		if err != nil {
			// Если запись в БД не удалась, пытаемся удалить скопированный файл
			if removeErr := os.Remove(destPath); removeErr != nil {
				log.Printf("Ошибка: не удалось удалить скопированный файл '%s' после ошибки БД: %v", destPath, removeErr)
			}
			dialog.ShowError(fmt.Errorf("не удалось сохранить информацию о вложении в БД: %w", err), a.window)
			return
		}

		dialog.ShowInformation("Успех", "Файл успешно прикреплен!", a.window)
		log.Printf("Файл '%s' прикреплен к заметке ID %d, сохранен как '%s'", originalFilename, selectedNote.ID, destPath)

		// Обновляем UI
		a.doSelectNote(a.selectedNoteIndex) // Перезагружаем заметку, чтобы обновить список вложений
	}, a.window)
}

// openAttachment открывает выбранный файл вложения с помощью системного приложения
// openAttachment открывает выбранный файл вложения с помощью системного приложения
func (a *NoteApp) openAttachment(attachment models.Attachment) {
	cmd := ""
	args := []string{}

	// Определяем команду для открытия файла в зависимости от ОС
	switch fyne.CurrentDevice() {
	// case "windows": //винда
	// 	cmd = "cmd"
	// 	args = []string{"/c", "start", attachment.Filepath}
	// case "darwin": //mac
	// 	cmd = "open"
	// 	args = []string{attachment.Filepath}
	default: // Linux и другие Unix-подобные
		cmd = "xdg-open"
		args = []string{attachment.Filepath}
	}

	command := exec.Command(cmd, args...)
	err := command.Start()
	if err != nil {
		dialog.ShowError(fmt.Errorf("не удалось открыть файл '%s': %w", attachment.Filename, err), a.window)
		log.Printf("Ошибка при открытии файла '%s' (%s): %v", attachment.Filename, attachment.Filepath, err)
	} else {
		log.Printf("Открыт файл '%s' (%s)", attachment.Filename, attachment.Filepath)
	}
}

// deleteAttachment удаляет выбранное вложение
func (a *NoteApp) deleteAttachment(attachment models.Attachment) {
	dialog.ShowConfirm("Подтверждение удаления",
		fmt.Sprintf("Вы уверены, что хотите удалить вложение '%s'? Файл будет удален с диска.", attachment.Filename),
		func(confirmed bool) {
			if confirmed {
				err := a.store.DeleteAttachment(attachment.ID)
				if err != nil {
					dialog.ShowError(fmt.Errorf("не удалось удалить вложение: %w", err), a.window)
					log.Printf("Ошибка при удалении вложения ID %d: %v", attachment.ID, err)
					return
				}
				dialog.ShowInformation("Успех", "Вложение успешно удалено.", a.window)
				log.Printf("Вложение ID %d ('%s') удалено.", attachment.ID, attachment.Filename)

				// Обновляем UI
				a.doSelectNote(a.selectedNoteIndex) // Перезагружаем заметку, чтобы обновить список вложений
			}
		}, a.window)
}

// formatBytes форматирует размер файла в удобочитаемый вид
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
