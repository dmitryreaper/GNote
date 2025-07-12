package ui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io/ioutil"
	"log"
	"mime"
	"os"
	"os/exec" // Для открытия файлов системным приложением
	"path/filepath"
	"runtime" // Импортируем пакет runtime для определения ОС
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"GNote/models"  // Модели данных
	"GNote/storage" // Интерфейс хранилища данных
)

// NoteApp представляет собой основную структуру приложения Fyne
type NoteApp struct {
	window fyne.Window
	store  storage.Store // Интерфейс для взаимодействия с БД

	allNotes          []models.Note // Все загруженные заметки (для фильтрации и сортировки)
	filteredNotes     []models.Note // Отфильтрованные заметки для отображения в списке UI
	selectedNoteIndex int           // Индекс выбранной заметки в filteredNotes (-1, если ничего не выбрано)
	hasUnsavedChanges bool          // Флаг для отслеживания несохраненных изменений в текущей заметке

	// UI элементы
	noteList       *widget.List
	searchEntry    *widget.Entry
	sortSelect     *widget.Select
	titleEntry     *widget.Entry
	contentEntry   *widget.Entry // ИСПРАВЛЕНО: тип должен быть *widget.Entry
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

	// Элементы для вложений
	attachmentsContainer *fyne.Container // Контейнер для списка вложений и кнопки "Прикрепить"
	attachmentsList      *widget.List    // Список отображаемых вложений
	attachButton         *widget.Button  // Кнопка для прикрепления файла
	attachmentsDirPath   string          // Путь к директории для хранения физических файлов вложений
}

// NewNoteApp создает новый экземпляр NoteApp
func NewNoteApp(w fyne.Window, s storage.Store) *NoteApp {
	app := &NoteApp{
		window:            w,
		store:             s,
		selectedNoteIndex: -1, // Изначально ничего не выбрано
		hasUnsavedChanges: false,
	}
	app.window.SetContent(app.MakeUI())
	app.window.SetMaster()                     // Устанавливаем окно как основное (для обработки закрытия)
	app.window.Resize(fyne.NewSize(1000, 700)) // Устанавливаем начальный размер окна
	app.window.SetOnClosed(app.onWindowClosed) // Обработчик закрытия окна

	// Определяем путь для хранения вложений
	// Используем Storage().RootURI().Path() для кроссплатформенного пути к данным приложения Fyne
	appDataPath := fyne.CurrentApp().Storage().RootURI().Path()
	app.attachmentsDirPath = filepath.Join(appDataPath, "attachments")
	// Создаем директорию для вложений, если она не существует
	if err := os.MkdirAll(app.attachmentsDirPath, 0755); err != nil { // 0755 - права доступа
		log.Printf("Ошибка при создании директории для вложений '%s': %v", app.attachmentsDirPath, err)
		dialog.ShowError(fmt.Errorf("не удалось создать директорию для вложений: %w", err), app.window)
	} else {
		log.Printf("Директория для вложений: %s", app.attachmentsDirPath)
	}

	// Загружаем заметки при старте приложения
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
		a.filterNotes() // При изменении текста поиска, фильтруем заметки
	}

	// Инициализируем a.noteList ДО a.sortSelect, так как a.sortSelect может вызвать Refresh a.noteList
	a.noteList = widget.NewList(
		func() int {
			return len(a.filteredNotes) // Количество элементов в списке
		},
		func() fyne.CanvasObject {
			// Шаблон для каждого элемента списка
			bg := canvas.NewRectangle(color.Transparent) // Фон для выделения
			label := widget.NewLabel("Название заметки") // Текст заметки
			return container.NewMax(bg, label)           // bg будет под label
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			// Обновление содержимого элемента списка
			note := a.filteredNotes[i]
			box := o.(*fyne.Container)
			bg := box.Objects[0].(*canvas.Rectangle)
			label := box.Objects[1].(*widget.Label)

			label.SetText(note.Title)

			// Визуальное выделение активной (выбранной) заметки
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
	a.noteList.OnSelected = a.onNoteSelected // Обработчик выбора заметки
	a.noteList.OnUnselected = func(id widget.ListItemID) {
		// При сбросе выделения, убедимся, что стиль сброшен для переиспользуемого элемента
		if id >= 0 && id < len(a.filteredNotes) {
			// Вызываем UpdateItem для сброса стиля (Fyne переиспользует объекты списка)
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
		a.sortNotes(s)       // Сортируем заметки
		a.noteList.Refresh() // Обновляем список UI
	})
	a.sortSelect.SetSelectedIndex(0) // Устанавливаем сортировку по умолчанию (вызовет коллбэк)

	leftPanel := container.NewBorder(
		container.NewVBox(a.searchEntry, a.sortSelect), // Поиск и сортировка сверху
		nil,        // Низ
		nil,        // Лево
		nil,        // Право
		a.noteList, // Центр (список заметок)
	)

	// --- Правая панель: Детали заметки и кнопки ---
	a.titleEntry = widget.NewEntry()
	a.titleEntry.SetPlaceHolder("Заголовок заметки")
	a.titleEntry.OnChanged = func(s string) {
		a.setUnsavedChanges(true) // Помечаем, что есть несохраненные изменения
	}

	a.contentEntry = widget.NewMultiLineEntry() // Инициализация MultiLineEntry
	a.contentEntry.SetPlaceHolder("Содержимое заметки...")
	a.contentEntry.Wrapping = fyne.TextWrapWord // Перенос слов
	a.contentEntry.OnChanged = func(s string) {
		a.setUnsavedChanges(true)
		a.updateCharCount() // Обновляем счетчик символов/слов
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
		a.updateReminderUI(nil) // Очищаем напоминание
	})
	reminderContainer := container.NewHBox(a.reminderLabel, a.reminderButton, clearReminderButton)

	// БЛОК: Вложения
	a.attachButton = widget.NewButtonWithIcon("Прикрепить файл", theme.ContentAddIcon(), a.attachFile)
	a.attachButton.Disable() // Изначально отключена, пока не выбрана/сохранена заметка

	a.attachmentsList = widget.NewList(
		func() int {
			selectedNote := a.getSelectedNote()
			if selectedNote == nil {
				return 0
			}
			return len(selectedNote.Attachments) // Количество вложений для выбранной заметки
		},
		func() fyne.CanvasObject {
			// Шаблон для каждого элемента вложения
			filenameLabel := widget.NewLabel("Имя файла")
			sizeLabel := widget.NewLabel("Размер")
			openButton := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), nil)
			deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)
			return container.NewHBox(filenameLabel, layout.NewSpacer(), sizeLabel, openButton, deleteButton)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			// Обновление содержимого элемента вложения
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
			sizeLabel.SetText(formatBytes(attachment.SizeBytes)) // Форматируем размер файла

			// Обработчики кнопок для каждого элемента списка вложений
			openButton.OnTapped = func() {
				a.openAttachment(attachment)
			}
			deleteButton.OnTapped = func() {
				a.deleteAttachment(attachment)
			}
		},
	)
	a.attachmentsContainer = container.NewBorder(
		container.NewHBox(widget.NewLabel("Вложения:"), layout.NewSpacer(), a.attachButton), // Заголовок и кнопка "Прикрепить" сверху
		nil,                                    // Низ
		nil,                                    // Лево
		nil,                                    // Право
		container.NewScroll(a.attachmentsList), // Список вложений с прокруткой
	)
	// КОНЕЦ БЛОКА ВЛОЖЕНИЙ

	a.saveButton = widget.NewButtonWithIcon("Сохранить", theme.DocumentSaveIcon(), a.saveNote)
	a.saveButton.Disable() // Изначально кнопка сохранения отключена

	a.deleteButton = widget.NewButtonWithIcon("Удалить", theme.DeleteIcon(), a.deleteNote)
	a.deleteButton.Disable() // Изначально кнопка удаления отключена

	newNoteButton := widget.NewButtonWithIcon("Новая заметка", theme.ContentAddIcon(), a.newNote)
	exportButton := widget.NewButtonWithIcon("Экспорт", theme.DownloadIcon(), a.exportNote)
	importButton := widget.NewButtonWithIcon("Импорт", theme.UploadIcon(), a.importNote)
	aboutButton := widget.NewButtonWithIcon("О программе", theme.InfoIcon(), a.showAboutDialog)

	// Контейнер для кнопок действий
	actionButtons := container.New(layout.NewGridLayoutWithColumns(4),
		newNoteButton, a.saveButton, a.deleteButton, exportButton,
		importButton, aboutButton,
	)

	// Контейнер для деталей заметки (правая панель)
	noteDetailContainer := container.NewBorder(
		container.NewVBox(
			a.titleEntry,
			a.tagsEntry,
			reminderContainer,
			widget.NewSeparator(),  // Разделитель
			a.attachmentsContainer, // <-- ДОБАВЛЕНО: Контейнер для вложений
			widget.NewSeparator(),  // Разделитель
		), // Заголовок, теги, напоминание, вложения сверху
		container.NewVBox(
			a.charCountLabel,
			actionButtons,
		), // Счетчик символов и кнопки снизу
		nil,                                 // Лево
		nil,                                 // Право
		container.NewScroll(a.contentEntry), // Содержимое с прокруткой в центре
	)

	// Горизонтальное разделение для списка и деталей
	split := container.NewHSplit(leftPanel, noteDetailContainer)
	split.SetOffset(0.25) // Список занимает 25% ширины окна

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
	notes, err := a.store.GetAllNotes() // Загружаем все заметки из БД
	if err != nil {
		dialog.ShowError(fmt.Errorf("не удалось загрузить заметки: %w", err), a.window)
		log.Printf("Ошибка при загрузке заметок: %v", err)
		return
	}
	a.allNotes = notes                 // Сохраняем все загруженные заметки
	a.filterNotes()                    // Применяем текущий фильтр
	a.sortNotes(a.sortSelect.Selected) // Применяем текущую сортировку
	a.noteList.Refresh()               // Обновляем UI список заметок
	log.Println("Заметки загружены и отфильтрованы/отсортированы")
}

// filterNotes фильтрует заметки на основе поискового запроса
func (a *NoteApp) filterNotes() {
	query := strings.ToLower(a.searchEntry.Text) // Получаем поисковый запрос в нижнем регистре
	if query == "" {
		a.filteredNotes = a.allNotes // Если запрос пуст, показываем все заметки
	} else {
		a.filteredNotes = []models.Note{}
		for _, note := range a.allNotes {
			// Проверяем наличие запроса в заголовке, содержимом или тегах
			if strings.Contains(strings.ToLower(note.Title), query) ||
				strings.Contains(strings.ToLower(note.Content), query) ||
				strings.Contains(strings.ToLower(strings.Join(note.Tags, ",")), query) { // Поиск по тегам (объединяем теги в строку)
				a.filteredNotes = append(a.filteredNotes, note)
			}
		}
	}
	a.sortNotes(a.sortSelect.Selected) // Пересортируем после фильтрации
	a.noteList.Refresh()               // Обновляем список UI

	// Если выбранная заметка больше не в отфильтрованном списке, сбросить выбор
	if a.selectedNoteIndex != -1 {
		selectedNote := a.getSelectedNote() // Получаем текущую выбранную заметку
		found := false
		for i, note := range a.filteredNotes {
			if selectedNote != nil && note.ID == selectedNote.ID {
				a.selectedNoteIndex = i // Обновляем индекс, если заметка все еще в списке
				a.noteList.Select(i)    // Выделяем ее в списке
				found = true
				break
			}
		}
		if !found {
			a.noteList.UnselectAll() // Снимаем выделение
			a.selectedNoteIndex = -1
			a.newNote() // Очищаем поля, если выбранная заметка пропала из фильтра
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

// getSelectedNote возвращает указатель на выбранную заметку или nil, если ничего не выбрано
func (a *NoteApp) getSelectedNote() *models.Note {
	if a.selectedNoteIndex == -1 || a.selectedNoteIndex >= len(a.filteredNotes) {
		return nil
	}
	return &a.filteredNotes[a.selectedNoteIndex]
}

// onNoteSelected вызывается при выборе заметки из списка
func (a *NoteApp) onNoteSelected(id widget.ListItemID) {
	// Если есть несохраненные изменения, предлагаем сохранить
	if a.hasUnsavedChanges {
		a.showUnsavedChangesDialog(func() {
			a.doSelectNote(id) // Продолжаем выбор заметки после обработки изменений
		})
	} else {
		a.doSelectNote(id) // Выбираем заметку напрямую
	}
}

// doSelectNote выполняет фактический выбор заметки после проверки изменений
func (a *NoteApp) doSelectNote(id widget.ListItemID) {
	if id < 0 || id >= len(a.filteredNotes) {
		return // Некорректный ID
	}

	// Загружаем заметку с вложениями из БД, так как GetAllNotes их не загружает
	selectedNoteFromDB, err := a.store.GetNoteByID(a.filteredNotes[id].ID)
	if err != nil {
		dialog.ShowError(fmt.Errorf("не удалось загрузить детали заметки: %w", err), a.window)
		log.Printf("Ошибка при загрузке деталей заметки: %v", err)
		return
	}

	// Обновляем заметку в filteredNotes, чтобы она содержала вложения и другие актуальные данные
	a.filteredNotes[id] = *selectedNoteFromDB
	a.selectedNoteIndex = id
	selectedNote := a.filteredNotes[id] // Используем обновленную заметку

	// Обновляем UI поля
	a.titleEntry.SetText(selectedNote.Title)
	a.contentEntry.SetText(selectedNote.Content)
	a.tagsEntry.SetText(strings.Join(selectedNote.Tags, ", ")) // Теги в строку через запятую
	a.updateReminderUI(selectedNote.ReminderAt)                // Обновляем UI напоминания

	a.setUnsavedChanges(false)  // Сброс флага после загрузки новой заметки
	a.deleteButton.Enable()     // Включаем кнопку "Удалить"
	a.attachButton.Enable()     // Включаем кнопку "Прикрепить файл"
	a.updateCharCount()         // Обновить счетчик для выбранной заметки
	a.attachmentsList.Refresh() // Обновляем список вложений для выбранной заметки
	log.Printf("Выбрана заметка: %s (ID: %d)", selectedNote.Title, selectedNote.ID)

	// Обновляем визуальное выделение в списке (чтобы текущая заметка была выделена)
	a.noteList.Refresh()
}

// newNote очищает поля для создания новой заметки
func (a *NoteApp) newNote() {
	// Если есть несохраненные изменения, предлагаем сохранить
	if a.hasUnsavedChanges {
		a.showUnsavedChangesDialog(func() {
			a.doNewNote() // Продолжаем создание новой заметки после обработки изменений
		})
	} else {
		a.doNewNote() // Создаем новую заметку напрямую
	}
}

// doNewNote выполняет фактическое создание новой заметки после проверки изменений
func (a *NoteApp) doNewNote() {
	a.selectedNoteIndex = -1 // Указываем, что это новая заметка (нет ID)
	a.titleEntry.SetText("")
	a.contentEntry.SetText("")
	a.tagsEntry.SetText("")
	a.updateReminderUI(nil) // Сброс напоминания
	a.setUnsavedChanges(false)
	a.deleteButton.Disable() // Отключаем кнопку "Удалить" для новой заметки
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
	tags := parseTags(a.tagsEntry.Text) // Парсим теги из строки
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
	var currentNote *models.Note    // Для отслеживания сохраняемой/обновляемой заметки
	if a.getSelectedNote() == nil { // Новая заметка (ID не выбрано)
		note := &models.Note{
			Title:      title,
			Content:    content,
			Tags:       tags,
			ReminderAt: reminderAt,
		}
		err = a.store.CreateNote(note) // Создаем заметку в БД
		currentNote = note
		if err == nil {
			log.Printf("Создана новая заметка: %s (ID: %d)", note.Title, note.ID)
		}
	} else { // Обновление существующей заметки
		note := a.getSelectedNote()
		note.Title = title
		note.Content = content
		note.Tags = tags
		note.ReminderAt = reminderAt
		err = a.store.UpdateNote(note) // Обновляем заметку в БД
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
	a.deleteButton.Enable()    // Включаем кнопку "Удалить"
	a.attachButton.Enable()    // Включаем кнопку "Прикрепить файл" после сохранения
	a.loadNotes()              // Перезагружаем список, чтобы обновить/добавить заметку
	// Попытка снова выбрать заметку после обновления списка, чтобы обновить UI
	if currentNote != nil {
		for i, note := range a.filteredNotes {
			if note.ID == currentNote.ID {
				a.noteList.Select(i) // Выделяем заметку в списке
				// Убедимся, что selectedNoteIndex обновлен корректно
				a.selectedNoteIndex = i
				// Перезагружаем вложения для выбранной заметки после сохранения
				a.doSelectNote(i) // Это обновит вложения и другие детали
				break
			}
		}
	}
}

// deleteNote удаляет текущую выбранную заметку
func (a *NoteApp) deleteNote() {
	selectedNote := a.getSelectedNote()
	if selectedNote == nil {
		return // Ничего не выбрано для удаления
	}

	// Диалог подтверждения удаления
	dialog.ShowConfirm("Подтверждение удаления",
		fmt.Sprintf("Вы уверены, что хотите удалить заметку '%s'? Все связанные вложения также будут удалены с диска.", selectedNote.Title),
		func(confirmed bool) {
			if confirmed {
				err := a.store.DeleteNote(selectedNote.ID) // Удаляем заметку из БД и файлы вложений
				if err != nil {
					dialog.ShowError(fmt.Errorf("не удалось удалить заметку: %w", err), a.window)
					log.Printf("Ошибка при удалении заметки: %v", err)
					return
				}
				dialog.ShowInformation("Успех", "Заметка успешно удалена.", a.window)
				log.Printf("Удалена заметка с ID: %d", selectedNote.ID)
				a.loadNotes() // Перезагружаем список заметок
				a.newNote()   // Переходим к созданию новой заметки (очищаем поля)
			}
		}, a.window)
}

// updateCharCount обновляет счетчик символов и слов в содержимом заметки
func (a *NoteApp) updateCharCount() {
	content := a.contentEntry.Text
	chars := len(content)
	words := len(strings.Fields(content)) // Разделяем по пробелам и считаем слова
	a.charCountLabel.SetText(fmt.Sprintf("Символов: %d | Слов: %d", chars, words))
}

// showUnsavedChangesDialog показывает диалог подтверждения несохраненных изменений
func (a *NoteApp) showUnsavedChangesDialog(onContinue func()) {
	dialog.ShowConfirm("Несохраненные изменения",
		"У вас есть несохраненные изменения. Сохранить их?",
		func(save bool) {
			if save {
				a.saveNote() // Попытаться сохранить
				// В реальном приложении здесь нужна более сложная логика,
				// чтобы onContinue() вызывался только после успешного сохранения.
				// Для простоты, предполагаем, что пользователь хочет продолжить,
				// даже если сохранение не удалось (ошибка будет показана).
				onContinue()
			} else {
				a.setUnsavedChanges(false) // Отменить изменения (сбросить флаг)
				onContinue()
			}
		}, a.window)
}

// onWindowClosed обрабатывает закрытие окна приложения
func (a *NoteApp) onWindowClosed() {
	// Если есть несохраненные изменения, предлагаем сохранить перед закрытием
	if a.hasUnsavedChanges {
		a.showUnsavedChangesDialog(func() {
			// Если пользователь выбрал не сохранять или сохранил,
			// то закрываем приложение
			if !a.hasUnsavedChanges { // Если флаг сброшен, значит, сохранение прошло успешно или отменено
				a.window.Close()
			}
		})
	} else {
		a.window.Close() // Просто закрываем окно
	}
}

// parseTags преобразует строку тегов (разделенных запятыми) в срез строк
func parseTags(tagString string) []string {
	tags := strings.Split(tagString, ",")
	cleanTags := []string{}
	for _, tag := range tags {
		trimmedTag := strings.TrimSpace(tag) // Удаляем пробелы
		if trimmedTag != "" {
			cleanTags = append(cleanTags, trimmedTag)
		}
	}
	return cleanTags
}

// updateReminderUI обновляет отображение напоминания в UI
func (a *NoteApp) updateReminderUI(t *time.Time) {
	if t == nil {
		a.reminderLabel.SetText("Напоминание: Не установлено")
		a.currentReminder = nil // Сбрасываем временное хранилище
	} else {
		a.reminderLabel.SetText(fmt.Sprintf("Напоминание: %s", t.Format("02.01.2006 15:04")))
		a.currentReminder = t // Обновляем временное хранилище
	}
}

// setReminderDialog открывает диалог для установки напоминания
func (a *NoteApp) setReminderDialog() {
	// Инициализируем начальное время для диалога (текущее или уже установленное напоминание)
	initialTime := time.Now()
	if a.currentReminder != nil {
		initialTime = *a.currentReminder
	}

	a.reminderDateEntry = widget.NewEntry()
	a.reminderDateEntry.SetPlaceHolder("ДД.ММ.ГГГГ")
	a.reminderDateEntry.SetText(initialTime.Format("02.01.2006")) // Формат даты

	a.reminderTimeEntry = widget.NewEntry()
	a.reminderTimeEntry.SetPlaceHolder("ЧЧ:ММ")
	a.reminderTimeEntry.SetText(initialTime.Format("15:04")) // Формат времени

	// Кнопка для открытия календаря (использует встроенный календарь Fyne)
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

	// Показываем диалог с полями для ввода даты/времени и кнопками
	dialog.ShowCustomConfirm("Установить напоминание", "Установить", "Отмена", content, func(ok bool) {
		if ok { // Если пользователь нажал "Установить"
			dateStr := a.reminderDateEntry.Text
			timeStr := a.reminderTimeEntry.Text
			combinedStr := fmt.Sprintf("%s %s", dateStr, timeStr)

			// Парсим введенную строку в time.Time
			parsedTime, err := time.Parse("02.01.2006 15:04", combinedStr)
			if err != nil {
				dialog.ShowError(fmt.Errorf("неверный формат даты или времени. Используйте ДД.ММ.ГГГГ ЧЧ:ММ: %w", err), a.window)
				return
			}
			a.updateReminderUI(&parsedTime) // Обновляем UI напоминания
			a.setUnsavedChanges(true)       // Помечаем изменения
		}
	}, a.window)
}

// exportNote экспортирует выбранную заметку или все заметки в JSON файл
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
				notesToExport = []models.Note{*selectedNote} // Экспортируем только выбранную
			}

			// Диалог сохранения файла
			dialog.ShowFileSave(func(writer fyne.URIWriteCloser, err error) {
				if err != nil {
					dialog.ShowError(err, a.window)
					return
				}
				if writer == nil { // Пользователь отменил сохранение
					return
				}
				defer writer.Close()

				// Кодируем заметки в JSON с отступами для читаемости
				data, err := json.MarshalIndent(notesToExport, "", "  ")
				if err != nil {
					dialog.ShowError(fmt.Errorf("ошибка при форматировании JSON: %w", err), a.window)
					return
				}

				// Записываем данные в файл
				_, err = writer.Write(data)
				if err != nil {
					dialog.ShowError(fmt.Errorf("ошибка при записи файла: %w", err), a.window)
					return
				}
				dialog.ShowInformation("Экспорт", "Заметки успешно экспортированы!", a.window)
			}, a.window)
		}, a.window)
}

// importNote импортирует заметки из JSON файла
func (a *NoteApp) importNote() {
	// Диалог открытия файла
	dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if reader == nil { // Пользователь отменил
			return
		}
		defer reader.Close()

		// Читаем содержимое файла
		data, err := ioutil.ReadAll(reader)
		if err != nil {
			dialog.ShowError(fmt.Errorf("ошибка при чтении файла: %w", err), a.window)
			return
		}

		var importedNotes []models.Note
		// Декодируем JSON в срез заметок
		err = json.Unmarshal(data, &importedNotes)
		if err != nil {
			dialog.ShowError(fmt.Errorf("ошибка при парсинге JSON: %w", err), a.window)
			return
		}

		if len(importedNotes) == 0 {
			dialog.ShowInformation("Импорт", "В файле не найдено заметок для импорта.", a.window)
			return
		}

		// Диалог подтверждения импорта
		dialog.ShowConfirm("Импорт заметок",
			fmt.Sprintf("Вы уверены, что хотите импортировать %d заметки(ок)? Существующие заметки с такими же ID будут перезаписаны, а новые добавлены. Вложения будут импортированы, если файлы существуют по указанным путям.", len(importedNotes)),
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
						// Убедимся, что время напоминания в UTC, если это важно для БД
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
						// Убедимся, что время напоминания в UTC
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
						// Для реального импорта, нужно будет скопировать файлы в папку вложений приложения
						// и обновить filepath.
						// Сейчас просто создаем запись в БД, если файл существует по указанному пути.
						if _, err := os.Stat(attach.Filepath); err == nil {
							// Файл существует, создаем запись в БД
							attach.NoteID = note.ID // Привязываем к только что созданной/обновленной заметке
							if err := a.store.CreateAttachment(&attach); err != nil {
								log.Printf("Ошибка при импорте вложения '%s' для заметки ID %d: %v", attach.Filename, note.ID, err)
							}
						} else {
							log.Printf("Файл вложения '%s' не найден по пути '%s', запись не импортирована. Ошибка: %v", attach.Filename, attach.Filepath, err)
						}
					}
				}

				if importedCount > 0 {
					dialog.ShowInformation("Импорт", fmt.Sprintf("Успешно импортировано %d заметок.", importedCount), a.window)
					a.loadNotes() // Перезагружаем список после импорта
					a.newNote()   // Переходим к новой заметке
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
		// Используем ID заметки, текущее время и оригинальное имя файла
		uniqueFilename := fmt.Sprintf("%d_%s_%s", selectedNote.ID, time.Now().Format("20060102150405"), originalFilename)
		destPath := filepath.Join(a.attachmentsDirPath, uniqueFilename) // Путь для сохранения файла

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

		// Получаем MIME-тип файла по расширению
		mimeType := mime.TypeByExtension(filepath.Ext(originalFilename))
		if mimeType == "" {
			mimeType = "application/octet-stream" // Дефолтный тип, если не удалось определить
		}

		// Создаем запись о вложении в БД
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

		// Обновляем UI: перезагружаем заметку, чтобы обновить список вложений
		a.doSelectNote(a.selectedNoteIndex)
	}, a.window)
}

// openAttachment открывает выбранный файл вложения с помощью системного приложения
func (a *NoteApp) openAttachment(attachment models.Attachment) {
	cmd := ""
	args := []string{}

	// ИСПРАВЛЕНО: Используем runtime.GOOS для определения ОС
	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", attachment.Filepath}
	case "darwin": // macOS
		cmd = "open"
		args = []string{attachment.Filepath}
	default: // Linux и другие Unix-подобные
		cmd = "xdg-open" // Стандартная утилита для открытия файлов в Linux
		args = []string{attachment.Filepath}
	}

	command := exec.Command(cmd, args...)
	err := command.Start() // Запускаем команду
	if err != nil {
		dialog.ShowError(fmt.Errorf("не удалось открыть файл '%s': %w", attachment.Filename, err), a.window)
		log.Printf("Ошибка при открытии файла '%s' (%s): %v", attachment.Filename, attachment.Filepath, err)
	} else {
		log.Printf("Открыт файл '%s' (%s)", attachment.Filename, attachment.Filepath)
	}
}

// deleteAttachment удаляет выбранное вложение из БД и физический файл
func (a *NoteApp) deleteAttachment(attachment models.Attachment) {
	dialog.ShowConfirm("Подтверждение удаления",
		fmt.Sprintf("Вы уверены, что хотите удалить вложение '%s'? Файл будет удален с диска.", attachment.Filename),
		func(confirmed bool) {
			if confirmed {
				err := a.store.DeleteAttachment(attachment.ID) // Удаляем вложение из БД и файл
				if err != nil {
					dialog.ShowError(fmt.Errorf("не удалось удалить вложение: %w", err), a.window)
					log.Printf("Ошибка при удалении вложения ID %d: %v", attachment.ID, err)
					return
				}
				dialog.ShowInformation("Успех", "Вложение успешно удалено.", a.window)
				log.Printf("Вложение ID %d ('%s') удалено.", attachment.ID, attachment.Filename)

				// Обновляем UI: перезагружаем заметку, чтобы обновить список вложений
				a.doSelectNote(a.selectedNoteIndex)
			}
		}, a.window)
}

// formatBytes форматирует размер файла в удобочитаемый вид (например, 1.2 MB)
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
