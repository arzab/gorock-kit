package rockbus

// Subscription pairs a topic with a handler.
// Create one with On and pass it to NewApp.
type Subscription struct {
	topic   Topic
	handler Handler
}

// On creates a Subscription for the given topic and handler.
//
// Example:
//
//	var Subscriptions = []rockbus.Subscription{
//	    rockbus.On("user.created",  handlers.OnUserCreated),
//	    rockbus.On("order.placed",  handlers.OnOrderPlaced),
//	}
//
//	app := rockbus.NewApp(cfg, delivery.Subscriptions...)
func On(topic Topic, handler Handler) Subscription {
	return Subscription{topic: topic, handler: handler}
}
