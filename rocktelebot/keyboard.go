package rocktelebot

import tele "gopkg.in/telebot.v3"

// InlineKeyboard is a fluent builder for Telegram inline keyboards.
//
// Example:
//
//	kb := rocktelebot.NewInlineKeyboard()
//	c.Send("Choose:", kb.
//	    Row(kb.Data("✅ Yes", "confirm"), kb.Data("❌ No", "cancel")).
//	    Row(kb.URL("Open site", "https://example.com")).
//	    Markup(),
//	)
type InlineKeyboard struct {
	markup *tele.ReplyMarkup
	rows   []tele.Row
}

// NewInlineKeyboard creates a new inline keyboard builder.
func NewInlineKeyboard() *InlineKeyboard {
	return &InlineKeyboard{markup: &tele.ReplyMarkup{}}
}

// Data creates an inline button that sends callback data when pressed.
// unique must be unique across all buttons; data is passed to the OnCallback handler.
func (k *InlineKeyboard) Data(text, unique string, data ...string) tele.Btn {
	return k.markup.Data(text, unique, data...)
}

// URL creates an inline button that opens a URL when pressed.
func (k *InlineKeyboard) URL(text, url string) tele.Btn {
	return k.markup.URL(text, url)
}

// Row adds a row of buttons to the keyboard.
func (k *InlineKeyboard) Row(btns ...tele.Btn) *InlineKeyboard {
	k.rows = append(k.rows, k.markup.Row(btns...))
	return k
}

// Markup builds and returns the final ReplyMarkup ready to pass to c.Send().
func (k *InlineKeyboard) Markup() *tele.ReplyMarkup {
	k.markup.Inline(k.rows...)
	return k.markup
}

// ReplyKeyboard is a fluent builder for Telegram reply (custom) keyboards.
//
// Example:
//
//	kb := rocktelebot.NewReplyKeyboard()
//	c.Send("Choose:", kb.
//	    Row(kb.Text("📋 List"), kb.Text("➕ Add")).
//	    Row(kb.Text("❌ Cancel")).
//	    Markup(),
//	)
type ReplyKeyboard struct {
	markup *tele.ReplyMarkup
	rows   []tele.Row
}

// NewReplyKeyboard creates a new reply keyboard builder.
// Keyboard is resized by default.
func NewReplyKeyboard() *ReplyKeyboard {
	return &ReplyKeyboard{markup: &tele.ReplyMarkup{ResizeKeyboard: true}}
}

// Text creates a reply keyboard button with the given label.
func (k *ReplyKeyboard) Text(text string) tele.Btn {
	return k.markup.Text(text)
}

// Row adds a row of buttons to the keyboard.
func (k *ReplyKeyboard) Row(btns ...tele.Btn) *ReplyKeyboard {
	k.rows = append(k.rows, k.markup.Row(btns...))
	return k
}

// OneTime makes the keyboard disappear after the user presses a button.
func (k *ReplyKeyboard) OneTime() *ReplyKeyboard {
	k.markup.OneTimeKeyboard = true
	return k
}

// Markup builds and returns the final ReplyMarkup ready to pass to c.Send().
func (k *ReplyKeyboard) Markup() *tele.ReplyMarkup {
	k.markup.Reply(k.rows...)
	return k.markup
}

// RemoveKeyboard returns a ReplyMarkup that removes the custom keyboard.
func RemoveKeyboard() *tele.ReplyMarkup {
	return &tele.ReplyMarkup{RemoveKeyboard: true}
}
