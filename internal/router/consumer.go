package router

import (
	"github.com/z-alamsyah/codebase-go/internal/config"
	"github.com/z-alamsyah/codebase-go/internal/controller/consumer"
	"github.com/z-alamsyah/codebase-go/internal/model"
	"github.com/z-alamsyah/codebase-go/internal/platform/messaging"
)

type ConsumerDeps struct {
	App  config.App
	User *consumer.UserHandler
}

// ConsumerRoutes maps queues (and the routing keys they listen to) to
// controller handlers. Add one line per new consumer.
func ConsumerRoutes(d ConsumerDeps) []messaging.Route {
	return []messaging.Route{
		{Queue: d.App.Name + ".user.welcome-email", RoutingKey: model.EventUserCreated, Handler: d.User.HandleUserCreated},
	}
}
