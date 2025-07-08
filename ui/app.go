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
	tagsEntry      *widget.Entry // Для ввода тегов
	reminderButton *widget.Button // Для установки напоминания
	reminderLabel  *widget.Label  // Для отображения напоминания
	saveButton     *widget.Button
	deleteButton   *widget.Button

	// Для диалога напоминания
	reminderDateEntry *widget.Entry
	reminderTimeEntry *widget.Entry
	currentReminder   *time.Time // Временное хранилище для даты/времени напоминания в диалоге
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
	app.window.SetMaster() // Устанавливаем окно как основное
	app.window.Resize(fyne.NewSize(1000, 700)) // Устанавливаем начальный размер
	app.window.SetOnClosed(app.onWindowClosed) // Обработчик закрытия окна

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
	a.reminderButton = widget.NewButton("Установить напоминание", a.setReminderDialog) // Изменено
	clearReminderButton := widget.NewButton("Очистить", func() {
		a.setUnsavedChanges(true)
		a.updateReminderUI(nil)
	})
	reminderContainer := container.NewHBox(a.reminderLabel, a.reminderButton, clearReminderButton)

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
		), // Заголовок, теги, напоминание сверху
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

	a.selectedNoteIndex = id
	selectedNote := a.filteredNotes[id]

	a.titleEntry.SetText(selectedNote.Title)
	a.contentEntry.SetText(selectedNote.Content)
	a.tagsEntry.SetText(strings.Join(selectedNote.Tags, ", "))
	a.updateReminderUI(selectedNote.ReminderAt)

	a.setUnsavedChanges(false) // Сброс флага после загрузки
	a.deleteButton.Enable()
	a.updateCharCount() // Обновить счетчик для выбранной заметки
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
	a.noteList.UnselectAll() // Снимаем выделение со списка
	a.updateCharCount() // Обновить счетчик для пустой заметки
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
	a.loadNotes()           // Перезагружаем список, чтобы обновить/добавить заметку
	// Попытка снова выбрать заметку после обновления списка
	if currentNote != nil {
		for i, note := range a.filteredNotes {
			if note.ID == currentNote.ID {
				a.noteList.Select(i)
				// Убедимся, что selectedNoteIndex обновлен корректно
				a.selectedNoteIndex = i
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
		fmt.Sprintf("Вы уверены, что хотите удалить заметку '%s'?", selectedNote.Title),
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
			fmt.Sprintf("Вы уверены, что хотите импортировать %d заметки(ок)? Существующие заметки с такими же ID будут перезаписаны, а новые добавлены.", len(importedNotes)),
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
