package statsapi

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ToastNotifier is an interface for displaying desktop toast notifications to the user.
type ToastNotifier interface {
	ShowToast(ctx context.Context, title, message string) error
}

// WindowsToastNotifier dispatches native Windows desktop notifications via PowerShell WinRT APIs.
type WindowsToastNotifier struct {
	logger *slog.Logger
	mu     sync.Mutex
}

// NewWindowsToastNotifier creates a new WindowsToastNotifier.
func NewWindowsToastNotifier(logger *slog.Logger) *WindowsToastNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &WindowsToastNotifier{logger: logger}
}

// ShowToast displays a native Windows desktop notification.
func (n *WindowsToastNotifier) ShowToast(ctx context.Context, title, message string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.logger.Info("dispatching desktop toast notification",
		slog.String("title", title),
		slog.String("message", message),
	)

	// If not on Windows, log and return gracefully
	if runtime.GOOS != "windows" {
		n.logger.Info("[DESKTOP TOAST]", slog.String("title", title), slog.String("message", message))
		return nil
	}

	// Escape single quotes for PowerShell script
	safeTitle := strings.ReplaceAll(title, "'", "''")
	safeMessage := strings.ReplaceAll(message, "'", "''")

	script := fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$xml = "<toast><visual><binding template='ToastGeneric'><text>%s</text><text>%s</text></binding></visual></toast>"
$xmlDoc = [Windows.Data.Xml.Dom.XmlDocument]::new()
$xmlDoc.LoadXml($xml)
$toast = [Windows.UI.Notifications.ToastNotification]::new($xmlDoc)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe").Show($toast)
`, safeTitle, safeMessage)

	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if output, err := cmd.CombinedOutput(); err != nil {
		n.logger.Warn("failed to show Windows toast notification",
			slog.Any("error", err),
			slog.String("output", string(output)),
		)
		return fmt.Errorf("powershell toast error: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// NoopToastNotifier is a mock/noop notifier useful for testing and headless environments.
type NoopToastNotifier struct {
	mu            sync.Mutex
	Notifications []ToastRecord
}

// ToastRecord records a notification invocation.
type ToastRecord struct {
	Title     string
	Message   string
	Timestamp time.Time
}

// NewNoopToastNotifier constructs a NoopToastNotifier.
func NewNoopToastNotifier() *NoopToastNotifier {
	return &NoopToastNotifier{
		Notifications: make([]ToastRecord, 0),
	}
}

// ShowToast records the notification in-memory.
func (m *NoopToastNotifier) ShowToast(ctx context.Context, title, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Notifications = append(m.Notifications, ToastRecord{
		Title:     title,
		Message:   message,
		Timestamp: time.Now(),
	})
	return nil
}

// Count returns the number of dispatched notifications.
func (m *NoopToastNotifier) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Notifications)
}
