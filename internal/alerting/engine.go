package alerting

import (
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TraceData is the minimal trace information needed for alert evaluation.
type TraceData struct {
	ID          string
	TotalTokens uint32
	Model       string // derived from span GenAIRequestModel
}

// TraceFetcher is a function that returns traces created since a given time.
type TraceFetcher func(since time.Time) ([]TraceData, error)

// Notifier sends alert notifications.
type Notifier interface {
	Type() string
	Fire(rule Rule, state AlertState, trace TraceData) error
	Resolve(rule Rule, state AlertState, trace TraceData) error
}

// NotifierFactory creates a Notifier from configuration.
type NotifierFactory func(cfg NotifierConfig) (Notifier, error)

// NotifierRegistry maps channel types to their factories.
type NotifierRegistry struct {
	mu        sync.RWMutex
	factories map[string]NotifierFactory
}

// NewNotifierRegistry creates a new NotifierRegistry.
func NewNotifierRegistry() *NotifierRegistry {
	return &NotifierRegistry{factories: make(map[string]NotifierFactory)}
}

// Register adds a notifier factory for a channel type.
func (r *NotifierRegistry) Register(channelType string, factory NotifierFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[channelType] = factory
}

// Get returns a notifier factory for the given channel type.
func (r *NotifierRegistry) Get(channelType string) (NotifierFactory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.factories[channelType]
	return f, ok
}

// AlertEvent is emitted by the engine when a notification should be sent.
type AlertEvent struct {
	Rule     Rule
	State    AlertState
	Alert    AlertState // new/updated state
	Trace    TraceData  // trace data that triggered the event
	Notifier Notifier
	Action   string // "firing" or "resolved"
}

// Engine evaluates alert rules against trace data on a polling schedule.
type Engine struct {
	store    *RuleStore
	registry *NotifierRegistry

	mu       sync.Mutex
	lastEval map[string]time.Time // ruleID -> last evaluation time
}

// NewEngine creates a new evaluation engine.
func NewEngine(store *RuleStore, registry *NotifierRegistry) *Engine {
	return &Engine{
		store:    store,
		registry: registry,
		lastEval: make(map[string]time.Time),
	}
}

// Evaluate runs one evaluation cycle for all enabled rules.
// It returns alert events that need notification dispatch.
func (e *Engine) Evaluate(fetchTrace TraceFetcher) ([]AlertEvent, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	rules, err := e.store.ListRules()
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}

	now := time.Now()
	var events []AlertEvent

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		// Check if it's time to evaluate this rule.
		interval := time.Duration(rule.Interval) * time.Second
		if interval <= 0 {
			interval = 60 * time.Second
		}
		last, ok := e.lastEval[rule.ID]
		if ok && now.Sub(last) < interval {
			continue
		}
		e.lastEval[rule.ID] = now

		// Fetch traces since last evaluation.
		since := now.Add(-interval - time.Duration(rule.ForDuration)*time.Second)
		traces, err := fetchTrace(since)
		if err != nil {
			continue // skip rules whose fetcher fails
		}

		for _, trace := range traces {
			condMet := evaluateConditions(rule.Conditions, trace)

			// Load existing state.
			existingState, err := e.store.GetAlertState(rule.ID, trace.ID)
			if err != nil {
				continue
			}

			newStatus, shouldNotify := driveStateMachine(existingState, condMet, rule.ForDuration, now)

			// Build/update state record.
			var state AlertState
			if existingState != nil {
				state = *existingState
			} else {
				state = AlertState{
					ID:          uuid.New().String(),
					RuleID:      rule.ID,
					TraceIDHex:  trace.ID,
					TriggeredAt: now,
				}
			}

			oldStatus := state.Status
			state.Status = newStatus

			if newStatus == AlertFiring && oldStatus != AlertFiring {
				state.LastFiredAt = &now
			}
			if newStatus == AlertResolved {
				state.ResolvedAt = &now
			}

			if err := e.store.UpsertAlertState(state); err != nil {
				continue
			}

			if shouldNotify {
				factory, ok := e.registry.Get(rule.Notifier.Type)
				if !ok {
					continue
				}
				notifier, err := factory(rule.Notifier)
				if err != nil {
					continue
				}
				action := "firing"
				if newStatus == AlertResolved {
					action = "resolved"
				}
				events = append(events, AlertEvent{
					Rule:     rule,
					State:    state,
					Alert:    state,
					Trace:    trace,
					Notifier: notifier,
					Action:   action,
				})
			}
		}
	}

	return events, nil
}

// evaluateConditions returns true if all conditions match the trace (AND logic).
func evaluateConditions(conditions []Condition, trace TraceData) bool {
	if len(conditions) == 0 {
		return true
	}
	for _, c := range conditions {
		if !evaluateCondition(c, trace) {
			return false
		}
	}
	return true
}

func evaluateCondition(c Condition, trace TraceData) bool {
	var fieldValue interface{}
	switch c.Field {
	case "total_tokens":
		fieldValue = uint64(trace.TotalTokens)
	case "input_tokens":
		fieldValue = uint64(trace.TotalTokens)
	case "output_tokens":
		fieldValue = uint64(trace.TotalTokens)
	case "model":
		fieldValue = trace.Model
	default:
		return false
	}

	switch v := fieldValue.(type) {
	case uint64:
		threshold, err := strconv.ParseUint(c.Value, 10, 64)
		if err != nil {
			return false
		}
		return compareUint64(v, c.Op, threshold)
	case string:
		return compareString(v, c.Op, c.Value)
	default:
		return false
	}
}

func compareUint64(a uint64, op ConditionOperator, b uint64) bool {
	switch op {
	case OpGT:
		return a > b
	case OpGTE:
		return a >= b
	case OpLT:
		return a < b
	case OpLTE:
		return a <= b
	case OpEQ:
		return a == b
	case OpNEQ:
		return a != b
	default:
		return false
	}
}

func compareString(a string, op ConditionOperator, b string) bool {
	switch op {
	case OpEQ:
		return a == b
	case OpNEQ:
		return a != b
	default:
		return false
	}
}

// driveStateMachine determines the next state and whether to notify.
func driveStateMachine(existing *AlertState, condMet bool, forDuration int, now time.Time) (AlertStatus, bool) {
	currentStatus := AlertOK
	if existing != nil {
		currentStatus = existing.Status
	}

	switch currentStatus {
	case AlertOK, AlertResolved:
		if condMet {
			if forDuration <= 0 {
				return AlertFiring, true
			}
			return AlertPending, false
		}
		return currentStatus, false

	case AlertPending:
		if !condMet {
			return AlertOK, false
		}
		var triggeredAt time.Time
		if existing != nil {
			triggeredAt = existing.TriggeredAt
		}
		elapsed := now.Sub(triggeredAt)
		if elapsed >= time.Duration(forDuration)*time.Second {
			return AlertFiring, true
		}
		return AlertPending, false

	case AlertFiring:
		if !condMet {
			return AlertResolved, true
		}
		return AlertFiring, false

	default:
		return AlertOK, false
	}
}

// RunPolling starts the polling loop. It blocks until the done channel is closed.
func (e *Engine) RunPolling(done <-chan struct{}, fetchTrace TraceFetcher, onEvent func(AlertEvent)) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			events, err := e.Evaluate(fetchTrace)
			if err != nil {
				log.Printf("Alerting engine: evaluate error: %v", err)
				continue
			}
			for _, ev := range events {
				onEvent(ev)
			}
		case <-done:
			return
		}
	}
}
