package alerting

import (
	"context"
	"encoding/hex"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/labubu/labubu/internal/storage"
)

// Subsystem holds all alerting components.
type Subsystem struct {
	Store   *RuleStore
	Engine  *Engine
	Handler *AlertHandler

	done chan struct{}
}

// InitAlerting initializes the alerting subsystem.
// dbPath: path to the JSON store file (e.g. "data/alerting.json").
// traceStore: the trace storage for querying traces.
func InitAlerting(dbPath string, traceStore storage.Store) (*Subsystem, error) {
	if dbPath == "" {
		dbPath = "alerting.json"
	}

	store, err := NewRuleStore(dbPath)
	if err != nil {
		return nil, err
	}

	// Register notifiers.
	registry := NewNotifierRegistry()
	registry.Register("email", func(cfg NotifierConfig) (Notifier, error) {
		return NewEmailNotifier(cfg), nil
	})

	engine := NewEngine(store, registry)
	handler := NewAlertHandler(store)

	sub := &Subsystem{
		Store:   store,
		Engine:  engine,
		Handler: handler,
		done:    make(chan struct{}),
	}

	// Start polling loop.
	go engine.RunPolling(sub.done, func(since time.Time) ([]TraceData, error) {
		result, err := traceStore.ListTraces(context.Background(), storage.TraceQuery{
			Page:        1,
			PageSize:    500,
			StartTimeMS: uint64(since.UnixMilli()),
			EndTimeMS:   uint64(time.Now().UnixMilli()),
		})
		if err != nil {
			return nil, err
		}
		var traces []TraceData
		for _, t := range result.Traces {
			tokens := uint32(0)
			if t.TotalTokens != nil {
				tokens = *t.TotalTokens
			}
			// Extract model from trace span attributes.
			model := ""
			traceIDBytes, err := hex.DecodeString(t.TraceIDHex)
			if err == nil && len(traceIDBytes) == 16 {
				var traceID [16]byte
				copy(traceID[:], traceIDBytes)
				detail, err := traceStore.GetTrace(context.Background(), traceID)
				if err == nil && detail != nil {
					for _, span := range detail.Spans {
						if span.GenAIRequestModel != nil && *span.GenAIRequestModel != "" {
							model = *span.GenAIRequestModel
							break
						}
					}
				}
			}
			traces = append(traces, TraceData{
				ID:          t.TraceIDHex,
				TotalTokens: tokens,
				Model:       model,
			})
		}
		return traces, nil
	}, func(ev AlertEvent) {
		var err error
		if ev.Action == "firing" {
			err = ev.Notifier.Fire(ev.Rule, ev.Alert, ev.Trace)
		} else {
			err = ev.Notifier.Resolve(ev.Rule, ev.Alert, ev.Trace)
		}

		// Record notification history.
		success := err == nil
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		recipient := ""
		if len(ev.Rule.Notifier.Recipients) > 0 {
			recipient = ev.Rule.Notifier.Recipients[0]
		}
		histEntry := AlertNotification{
			ID:         NewUUID(),
			RuleID:     ev.Rule.ID,
			TraceIDHex: ev.Alert.TraceIDHex,
			Action:     ev.Action,
			Channel:    ev.Rule.Notifier.Type,
			Recipient:  recipient,
			SentAt:     time.Now(),
			Success:    success,
			ErrorMsg:   errMsg,
		}
		if err := store.InsertNotification(histEntry); err != nil {
			log.Printf("Alerting: failed to record notification: %v", err)
		}
		if !success {
			log.Printf("Alerting: notification failed: %v", err)
		}
	})

	log.Printf("Alerting: started (store=%s)", dbPath)
	return sub, nil
}

// Shutdown gracefully stops the alerting engine.
func (sub *Subsystem) Shutdown() {
	close(sub.done)
	if sub.Store != nil {
		sub.Store.Close()
	}
	log.Println("Alerting: stopped")
}

// NewUUID generates a new UUID string. Wraps google/uuid for easy use.
func NewUUID() string {
	return uuid.New().String()
}
