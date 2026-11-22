package services

import (
	"context"
	"errors"
	"log"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// NotifyService wraps Wails' notification service so a build that may not post notifications
// (on macOS: an unbundled or unsigned binary, such as `wails3 dev`) still starts. Alerts are
// then only shown inside the app.
type NotifyService struct {
	n  *notifications.NotificationService
	ok atomic.Bool
}

func NewNotifyService() *NotifyService { return &NotifyService{n: notifications.New()} }

func (s *NotifyService) ServiceName() string { return "NotifyService" }

func (s *NotifyService) ServiceStartup(ctx context.Context, opts application.ServiceOptions) error {
	if err := s.n.ServiceStartup(ctx, opts); err != nil {
		log.Printf("notifications unavailable: %v", err)
		return nil
	}
	s.ok.Store(true)
	return nil
}

func (s *NotifyService) ServiceShutdown() error {
	if s.ok.Load() {
		return s.n.ServiceShutdown()
	}
	return nil
}

var errNoNotifications = errors.New("this build can't post system notifications")

// Available reports whether OS notifications can be posted.
func (s *NotifyService) Available() bool { return s.ok.Load() }

// Authorize asks the OS for permission to post notifications.
func (s *NotifyService) Authorize() (bool, error) {
	if !s.ok.Load() {
		return false, errNoNotifications
	}
	return s.n.RequestNotificationAuthorization()
}

// Send posts one notification.
func (s *NotifyService) Send(id, title, body string) error {
	if !s.ok.Load() {
		return errNoNotifications
	}
	return s.n.SendNotification(notifications.NotificationOptions{ID: id, Title: title, Body: body, ThreadID: "claudealyzer-alerts"})
}
