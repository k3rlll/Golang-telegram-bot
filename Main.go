package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	telegram "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Trainer struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Bio          string   `json:"bio"`
	Achievements []string `json:"achievements"`
	Slots        []string `json:"slots"`
}

type Booking struct {
	UserID   int64  `json:"user_id"`
	Trainer  int    `json:"trainer"`
	TimeSlot string `json:"time_slot"`
	BookedAt int64  `json:"booked_at"`
}

type User struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	HasPaid bool   `json:"has_paid"`
}

type AppState struct {
	Users    map[int64]*User `json:"users"`
	Trainers []Trainer       `json:"trainers"`
	Bookings []Booking       `json:"bookings"`
}

var (
	state     AppState
	stateMu   sync.Mutex
	statePath = filepath.Join(".", "state.json")
	gymName   = "Alfa Fitness"
	priceText = "Прайсы абонементов (тенге):\n\n" +
		"• Gold — 25 000 ₸ / мес\n" +
		"• Silver — 18 000 ₸ / мес\n" +
		"• Bronze — 12 000 ₸ / мес\n" +
		"• Студенческий — 9 000 ₸ / мес\n\n" +
		"Нажмите \"Оплатить\" для симуляции оплаты."
)

func defaultSlots() []string {
	return []string{"08:00", "09:00", "10:00", "11:00", "12:00", "13:00", "14:00", "15:00", "16:00", "17:00", "18:00", "19:00", "20:00"}
}

func defaultTrainers() []Trainer {
	return []Trainer{
		{ID: 1, Name: "Айдос Нуртаев", Bio: "Силовой тренинг, функциональная подготовка.", Achievements: []string{"МС по пауэрлифтингу", "Победитель Almaty Open 2022"}, Slots: defaultSlots()},
		{ID: 2, Name: "Алия Жаксылыкова", Bio: "Фитнес для женщин, послеродовое восстановление.", Achievements: []string{"Сертифицированный персональный тренер NASM"}, Slots: defaultSlots()},
		{ID: 3, Name: "Расул Абдрахман", Bio: "Бокс, ОФП, выносливость.", Achievements: []string{"Чемпион РК среди юниоров по боксу"}, Slots: defaultSlots()},
		{ID: 4, Name: "Динара Есмухан", Bio: "Йога, гибкость, дыхательные практики.", Achievements: []string{"RYT-500 Yoga Alliance"}, Slots: defaultSlots()},
		{ID: 5, Name: "Мади Бекен", Bio: "Кроссфит, снижение веса.", Achievements: []string{"Сертифицированный тренер CrossFit L1"}, Slots: defaultSlots()},
	}
}

func loadState() error {
	f, err := os.Open(statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			tmp := AppState{
				Users:    map[int64]*User{},
				Trainers: defaultTrainers(),
				Bookings: []Booking{},
			}
			stateMu.Lock()
			state = tmp
			stateMu.Unlock()
			return saveState()
		}
		return err
	}
	defer f.Close()

	var tmp AppState
	dec := json.NewDecoder(f)
	if err := dec.Decode(&tmp); err != nil {
		return err
	}
	if len(tmp.Trainers) == 0 {
		tmp.Trainers = defaultTrainers()
	}
	if tmp.Users == nil {
		tmp.Users = map[int64]*User{}
	}

	stateMu.Lock()
	state = tmp
	stateMu.Unlock()
	return nil
}

func saveState() error {
	stateMu.Lock()
	defer stateMu.Unlock()

	tmp := state
	b, err := json.MarshalIndent(&tmp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, b, 0644)
}

func getOrCreateUser(id int64, name string) *User {
	stateMu.Lock()
	defer stateMu.Unlock()
	u, ok := state.Users[id]
	if !ok {
		u = &User{ID: id, Name: name, HasPaid: false}
		state.Users[id] = u
	}
	return u
}

func getTrainerByID(id int) (*Trainer, int) {
	stateMu.Lock()
	defer stateMu.Unlock()
	for i := range state.Trainers {
		if state.Trainers[i].ID == id {
			return &state.Trainers[i], i
		}
	}
	return nil, -1
}

func bookSlot(userID int64, trainerID int, slot string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	var existingTrainer int
	userCountWithThisTrainer := 0
	for _, b := range state.Bookings {
		if b.UserID != userID {
			continue
		}
		if existingTrainer == 0 {
			existingTrainer = b.Trainer
		}
		if b.Trainer == trainerID {
			userCountWithThisTrainer++
		} else if trainerID != existingTrainer {
			return fmt.Errorf("вы уже записаны к другому тренеру. Можно записываться только к одному тренеру.")
		}
	}
	if userCountWithThisTrainer >= 3 {
		return fmt.Errorf("лимит: максимум 3 записи у одного тренера.")
	}

	idx := -1
	for i := range state.Trainers {
		if state.Trainers[i].ID == trainerID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("тренер не найден")
	}

	pos := -1
	for i, s := range state.Trainers[idx].Slots {
		if s == slot {
			pos = i
			break
		}
	}
	if pos == -1 {
		return fmt.Errorf("слот уже занят или не существует")
	}

	slots := state.Trainers[idx].Slots
	state.Trainers[idx].Slots = append(slots[:pos], slots[pos+1:]...)

	state.Bookings = append(state.Bookings, Booking{
		UserID:   userID,
		Trainer:  trainerID,
		TimeSlot: slot,
		BookedAt: time.Now().Unix(),
	})

	return nil
}

func mainMenuKeyboard() telegram.ReplyKeyboardMarkup {
	return telegram.NewReplyKeyboard(
		telegram.NewKeyboardButtonRow(
			telegram.NewKeyboardButton("Тренеры"),
			telegram.NewKeyboardButton("Прайс абонементов"),
		),
	)
}

func trainersInlineKeyboard(hasPaid bool) telegram.InlineKeyboardMarkup {
	stateMu.Lock()
	trainers := make([]Trainer, len(state.Trainers))
	copy(trainers, state.Trainers)
	stateMu.Unlock()

	rows := [][]telegram.InlineKeyboardButton{}
	for _, t := range trainers {
		row := []telegram.InlineKeyboardButton{
			telegram.NewInlineKeyboardButtonData("👤 "+t.Name, fmt.Sprintf("trainer_%d", t.ID)),
		}
		if hasPaid {
			row = append(row, telegram.NewInlineKeyboardButtonData("🗓 Запись", fmt.Sprintf("book_%d", t.ID)))
		}
		rows = append(rows, row)
	}
	rows = append(rows, []telegram.InlineKeyboardButton{telegram.NewInlineKeyboardButtonData("⬅️ В меню", "menu")})
	return telegram.NewInlineKeyboardMarkup(rows...)
}

func trainerDetailsKeyboard(t Trainer, hasPaid bool) telegram.InlineKeyboardMarkup {
	row := []telegram.InlineKeyboardButton{}
	if hasPaid {
		row = append(row, telegram.NewInlineKeyboardButtonData("🗓 Запись", fmt.Sprintf("book_%d", t.ID)))
	}
	row = append(row, telegram.NewInlineKeyboardButtonData("⬅️ Назад", "trainers"))
	return telegram.NewInlineKeyboardMarkup(row)
}

func scheduleKeyboard(trainerID int) telegram.InlineKeyboardMarkup {
	stateMu.Lock()
	slots := append([]string{}, state.Trainers[trainerID-1].Slots...)
	stateMu.Unlock()

	rows := [][]telegram.InlineKeyboardButton{}
	row := []telegram.InlineKeyboardButton{}
	for i, s := range slots {
		row = append(row, telegram.NewInlineKeyboardButtonData(s, fmt.Sprintf("slot_%d_%s", trainerID, s)))
		if (i+1)%4 == 0 {
			rows = append(rows, row)
			row = []telegram.InlineKeyboardButton{}
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []telegram.InlineKeyboardButton{telegram.NewInlineKeyboardButtonData("⬅️ Назад", "trainers")})
	return telegram.NewInlineKeyboardMarkup(rows...)
}

func pricingKeyboard() telegram.InlineKeyboardMarkup {
	return telegram.NewInlineKeyboardMarkup(
		telegram.NewInlineKeyboardRow(
			telegram.NewInlineKeyboardButtonData("Оплатить Gold (25 000 ₸)", "pay_gold"),
		),
		telegram.NewInlineKeyboardRow(
			telegram.NewInlineKeyboardButtonData("Оплатить Silver (18 000 ₸)", "pay_silver"),
		),
		telegram.NewInlineKeyboardRow(
			telegram.NewInlineKeyboardButtonData("Оплатить Bronze (12 000 ₸)", "pay_bronze"),
		),
		telegram.NewInlineKeyboardRow(
			telegram.NewInlineKeyboardButtonData("Оплатить Студенческий (9 000 ₸)", "pay_student"),
		),
		telegram.NewInlineKeyboardRow(
			telegram.NewInlineKeyboardButtonData("⬅️ В меню", "menu"),
		),
	)
}

func main() {
	if err := loadState(); err != nil {
		log.Fatalf("load state: %v", err)
	}

	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_TOKEN is not set")
	}

	bot, err := telegram.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}
	bot.Debug = false
	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := telegram.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			userID := update.Message.From.ID
			name := strings.TrimSpace(update.Message.From.FirstName + " " + update.Message.From.LastName)
			if name == "" {
				name = update.Message.From.UserName
			}

			user := getOrCreateUser(userID, name)

			if update.Message.IsCommand() || update.Message.Text == "/start" {
				welcome := fmt.Sprintf("Вас приветствует фитнес зал %s!\nВыберите раздел ниже.", gymName)
				msg := telegram.NewMessage(update.Message.Chat.ID, welcome)
				msg.ReplyMarkup = mainMenuKeyboard()
				_ = send(bot, msg)
				continue
			}

			switch update.Message.Text {
			case "Тренеры":
				msg := telegram.NewMessage(update.Message.Chat.ID, "Наши тренеры:")
				msg.ReplyMarkup = mainMenuKeyboard()
				msg.ReplyMarkup = nil
				msg.Text = "Наши тренеры (нажмите имя, чтобы узнать подробнее):"
				msgReply := telegram.NewMessage(update.Message.Chat.ID, msg.Text)
				msgReply.ReplyMarkup = trainersInlineKeyboard(user.HasPaid)
				_ = send(bot, msgReply)
			case "Прайс абонементов":
				msg := telegram.NewMessage(update.Message.Chat.ID, priceText)
				msg.ReplyMarkup = pricingKeyboard()
				_ = send(bot, msg)
			default:
				msg := telegram.NewMessage(update.Message.Chat.ID, "Не понял команду. Пожалуйста, выберите пункт меню.")
				msg.ReplyMarkup = mainMenuKeyboard()
				_ = send(bot, msg)
			}
		}

		if update.CallbackQuery != nil {
			cq := update.CallbackQuery
			userID := cq.From.ID
			user := getOrCreateUser(userID, cq.From.FirstName)

			data := cq.Data
			_ = answerCallback(bot, cq.ID, "")

			if data == "menu" {
				m := telegram.NewMessage(cq.Message.Chat.ID, fmt.Sprintf("Вас приветствует фитнес зал %s!", gymName))
				m.ReplyMarkup = mainMenuKeyboard()
				_ = send(bot, m)
				continue
			}
			if data == "trainers" {
				m := telegram.NewMessage(cq.Message.Chat.ID, "Наши тренеры (нажмите имя, чтобы узнать подробнее):")
				m.ReplyMarkup = trainersInlineKeyboard(user.HasPaid)
				_ = send(bot, m)
				continue
			}

			if strings.HasPrefix(data, "trainer_") {
				idStr := strings.TrimPrefix(data, "trainer_")
				var id int
				fmt.Sscanf(idStr, "%d", &id)
				tr, _ := getTrainerByID(id)
				if tr == nil {
					_ = send(bot, telegram.NewMessage(cq.Message.Chat.ID, "Тренер не найден"))
					continue
				}
				text := fmt.Sprintf("%s\n\nОписание: %s\n\nДостижения:\n• %s", tr.Name, tr.Bio, strings.Join(tr.Achievements, "\n• "))
				m := telegram.NewMessage(cq.Message.Chat.ID, text)
				m.ReplyMarkup = trainerDetailsKeyboard(*tr, user.HasPaid)
				_ = send(bot, m)
				continue
			}

			if strings.HasPrefix(data, "book_") {
				if !user.HasPaid {
					_ = send(bot, telegram.NewMessage(cq.Message.Chat.ID, "Чтобы записаться, сначала оплатите абонемент в разделе \"Прайс абонементов\"."))
					continue
				}
				idStr := strings.TrimPrefix(data, "book_")
				var id int
				fmt.Sscanf(idStr, "%d", &id)
				tr, _ := getTrainerByID(id)
				if tr == nil {
					_ = send(bot, telegram.NewMessage(cq.Message.Chat.ID, "Тренер не найден"))
					continue
				}
				m := telegram.NewMessage(cq.Message.Chat.ID, fmt.Sprintf("Выберите время для тренера %s:", tr.Name))
				m.ReplyMarkup = scheduleKeyboard(tr.ID)
				_ = send(bot, m)
				continue
			}

			if strings.HasPrefix(data, "slot_") {
				parts := strings.SplitN(strings.TrimPrefix(data, "slot_"), "_", 2)
				if len(parts) != 2 {
					continue
				}
				var trainerID int
				fmt.Sscanf(parts[0], "%d", &trainerID)
				slot := parts[1]

				if !user.HasPaid {
					_ = send(bot, telegram.NewMessage(cq.Message.Chat.ID, "Сначала оплатите абонемент."))
					continue
				}

				if err := bookSlot(userID, trainerID, slot); err != nil {
					_ = send(bot, telegram.NewMessage(cq.Message.Chat.ID, "Не удалось записаться: "+err.Error()))
					continue
				}
				_ = saveState()

				confirm := fmt.Sprintf("Запись подтверждена! Тренер #%d, время %s.", trainerID, slot)
				_ = send(bot, telegram.NewMessage(cq.Message.Chat.ID, confirm))

				tr, _ := getTrainerByID(trainerID)
				m := telegram.NewMessage(cq.Message.Chat.ID, fmt.Sprintf("Свободные слоты у %s обновлены:", tr.Name))
				m.ReplyMarkup = scheduleKeyboard(tr.ID)
				_ = send(bot, m)
				continue
			}

			if strings.HasPrefix(data, "pay_") {
				stateMu.Lock()
				state.Users[userID].HasPaid = true
				stateMu.Unlock()
				_ = saveState()

				_ = send(bot, telegram.NewMessage(cq.Message.Chat.ID, "Операция прошла успешно!"))
				m := telegram.NewMessage(cq.Message.Chat.ID, "Теперь вы можете записаться к тренеру в разделе \"Тренеры\":")
				m.ReplyMarkup = trainersInlineKeyboard(true)
				_ = send(bot, m)
				continue
			}
		}
	}
}

func send(bot *telegram.BotAPI, msg telegram.Chattable) error {
	_, err := bot.Send(msg)
	if err != nil {
		log.Printf("send error: %v", err)
	}
	return err
}

func answerCallback(bot *telegram.BotAPI, id string, text string) error {
	cb := telegram.NewCallback(id, text)
	_, err := bot.Request(cb)
	return err
}
