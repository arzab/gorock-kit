package rocktelebot

import tele "gopkg.in/telebot.v3"

// Handler pairs a trigger entity with a handler function and optional middlewares.
// Create one with Command or GetHandler and pass it to NewApp.
type Handler struct {
	entity      interface{}
	handler     tele.HandlerFunc
	middlewares []tele.MiddlewareFunc
	desc        string
}

// WithDesc attaches a description shown in the Telegram commands menu.
// Only meaningful for Command handlers.
//
// Example:
//
//	rocktelebot.Command("/start", onStart).WithDesc("Start the bot")
func (h Handler) WithDesc(desc string) Handler {
	h.desc = desc
	return h
}

// Command registers a handler for a bot command, e.g. "/start".
//
// Example:
//
//	rocktelebot.Command("/start", onStart)
//	rocktelebot.Command("/help",  onHelp, authMiddleware)
func Command(cmd string, h tele.HandlerFunc, middlewares ...tele.MiddlewareFunc) Handler {
	return Handler{entity: cmd, handler: h, middlewares: middlewares}
}

// GetHandler registers a handler for any telebot entity.
//
// Examples:
//
//	rocktelebot.GetHandler(tele.OnText,     onText)
//	rocktelebot.GetHandler(tele.OnPhoto,    onPhoto)
//	rocktelebot.GetHandler(tele.OnCallback, onCallback)
//	rocktelebot.GetHandler(tele.OnVoice,    onVoice)
func GetHandler(entity interface{}, h tele.HandlerFunc, middlewares ...tele.MiddlewareFunc) Handler {
	return Handler{entity: entity, handler: h, middlewares: middlewares}
}
