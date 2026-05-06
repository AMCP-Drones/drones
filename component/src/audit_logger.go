package component

import "context"

// AuditLogger publishes LOG_EVENT entries through security_monitor.
type AuditLogger struct {
	Proxy        *ProxyClient
	JournalTopic string
	Source       string
}

// LogEvent sends a structured audit event.
func (a *AuditLogger) LogEvent(ctx context.Context, event string, details map[string]interface{}) error {
	if details == nil {
		details = map[string]interface{}{}
	}
	return a.Proxy.ProxyPublishAsync(ctx, a.JournalTopic, "LOG_EVENT", map[string]interface{}{
		"event":   event,
		"source":  a.Source,
		"details": details,
	})
}
