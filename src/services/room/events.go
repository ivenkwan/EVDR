package room

import (
	"context"
	"log/slog"
	"time"

	"github.com/ivenkwan/evdr/src/spi"
)

// Event is one audit-worthy decision made by the service: who did what to
// which target, with what result, in which tenant. The platform's audit of
// record is the append-only PostgreSQL store fed from NATS JetStream
// (TR-7.1, a later wave); this type is the seam those emissions will be
// built from. Every event must carry TenantID — no silent data paths
// (AGENTS.md §6).
type Event struct {
	// Action is a stable machine-readable verb, e.g. "room.create",
	// "doc.upload", "grant.revoke", "branding.public".
	Action string
	// Target is the resource the action applied to (room/document/grant id,
	// or "-" when not applicable).
	Target string
	// TenantID is the tenant scope of the action.
	TenantID spi.TenantID
	// Actor is the authenticated principal that performed the action.
	Actor spi.Actor
	// Result is "ok" for success or the error code (e.g. "room_sealed") for
	// failure. Errors carry Detail with the underlying message.
	Result string
	// Detail is optional structured context for the event.
	Detail map[string]string
	// At is when the action was decided.
	At time.Time
}

// EventSink consumes structured audit events. Implementations must be safe
// for concurrent use and must not block request handling indefinitely. The
// shipped SlogEventSink logs via log/slog; production wires a NATS JetStream
// publisher (TR-7.1) behind the same interface.
type EventSink interface {
	Emit(ctx context.Context, ev Event)
}

// SlogEventSink emits events as structured log records with severity levels.
// Field names follow the platform's audit vocabulary (tenant_id, actor,
// action, target, result) so they can be routed to Loki/Fluent Bit today and
// to the NATS/ClickHouse pipeline later without reshaping.
type SlogEventSink struct {
	logger *slog.Logger
}

// NewSlogEventSink returns an EventSink that logs every event at Info level.
func NewSlogEventSink(logger *slog.Logger) *SlogEventSink {
	return &SlogEventSink{logger: logger}
}

// Emit logs ev as a structured record. Failures are logged as errors; the
// success path is Info. ctx is honoured for log cancellation.
func (s *SlogEventSink) Emit(ctx context.Context, ev Event) {
	attrs := []any{
		"tenant_id", string(ev.TenantID),
		"actor", ev.Actor.ID,
		"actor_kind", string(ev.Actor.Kind),
		"action", ev.Action,
		"target", ev.Target,
		"result", ev.Result,
	}
	if len(ev.Detail) > 0 {
		for k, v := range ev.Detail {
			attrs = append(attrs, k, v)
		}
	}
	if ev.Result != "ok" {
		s.logger.ErrorContext(ctx, "room decision failed", attrs...)
		return
	}
	s.logger.InfoContext(ctx, "room decision", attrs...)
}
