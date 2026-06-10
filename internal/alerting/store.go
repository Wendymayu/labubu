package alerting

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
)

// RuleStore persists rules, alert states, and notification history to a JSON file.
// Uses a simple file-based store suitable for the single-user local-first deployment.
// Can be swapped to SQLite later without changing the interface.
type RuleStore struct {
	mu     sync.RWMutex
	dbPath string

	rules         map[string]Rule
	states        map[string]AlertState   // key: ruleID + ":" + traceIDHex
	notifications []AlertNotification
}

// NewRuleStore creates or opens a rule store at the given path.
func NewRuleStore(dbPath string) (*RuleStore, error) {
	s := &RuleStore{
		dbPath:        dbPath,
		rules:         make(map[string]Rule),
		states:        make(map[string]AlertState),
		notifications: make([]AlertNotification, 0),
	}
	// Load existing data if file exists.
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load store: %w", err)
	}
	return s, nil
}

func (s *RuleStore) Close() error {
	return s.save()
}

// persistedData is the on-disk format.
type persistedData struct {
	Rules         []Rule               `json:"rules"`
	States        []AlertState         `json:"states"`
	Notifications []AlertNotification  `json:"notifications"`
}

func (s *RuleStore) load() error {
	data, err := os.ReadFile(s.dbPath)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil // fresh empty file
	}
	var pd persistedData
	if err := json.Unmarshal(data, &pd); err != nil {
		return fmt.Errorf("unmarshal store: %w", err)
	}
	for _, r := range pd.Rules {
		s.rules[r.ID] = r
	}
	for _, st := range pd.States {
		key := st.RuleID + ":" + st.TraceIDHex
		s.states[key] = st
	}
	s.notifications = pd.Notifications
	if s.notifications == nil {
		s.notifications = make([]AlertNotification, 0)
	}
	return nil
}

func (s *RuleStore) save() error {
	s.mu.RLock()
	rules := make([]Rule, 0, len(s.rules))
	for _, r := range s.rules {
		rules = append(rules, r)
	}
	states := make([]AlertState, 0, len(s.states))
	for _, st := range s.states {
		states = append(states, st)
	}
	notifications := s.notifications
	s.mu.RUnlock()

	pd := persistedData{
		Rules:         rules,
		States:        states,
		Notifications: notifications,
	}
	data, err := json.MarshalIndent(pd, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal store: %w", err)
	}
	return os.WriteFile(s.dbPath, data, 0644)
}

// --- Rule CRUD ---

func (s *RuleStore) CreateRule(r Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rules[r.ID]; exists {
		return fmt.Errorf("rule %q already exists", r.ID)
	}
	s.rules[r.ID] = r
	return s.saveLocked()
}

func (s *RuleStore) GetRule(id string) (*Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rules[id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (s *RuleStore) ListRules() ([]Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rules := make([]Rule, 0, len(s.rules))
	for _, r := range s.rules {
		rules = append(rules, r)
	}
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].CreatedAt.After(rules[j].CreatedAt)
	})
	return rules, nil
}

func (s *RuleStore) UpdateRule(r Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rules[r.ID]; !exists {
		return fmt.Errorf("rule %q not found", r.ID)
	}
	s.rules[r.ID] = r
	return s.saveLocked()
}

func (s *RuleStore) DeleteRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rules, id)
	return s.saveLocked()
}

// --- Alert State ---

func (s *RuleStore) UpsertAlertState(state AlertState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := state.RuleID + ":" + state.TraceIDHex
	s.states[key] = state
	return s.saveLocked()
}

func (s *RuleStore) GetAlertState(ruleID, traceIDHex string) (*AlertState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := ruleID + ":" + traceIDHex
	st, ok := s.states[key]
	if !ok {
		return nil, nil
	}
	return &st, nil
}

func (s *RuleStore) ListAlertStates(statusFilter string) ([]AlertState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	states := make([]AlertState, 0)
	for _, st := range s.states {
		if statusFilter != "" && string(st.Status) != statusFilter {
			continue
		}
		states = append(states, st)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].TriggeredAt.After(states[j].TriggeredAt)
	})
	return states, nil
}

// --- Notification History ---

func (s *RuleStore) InsertNotification(n AlertNotification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifications = append(s.notifications, n)
	return s.saveLocked()
}

func (s *RuleStore) ListNotifications(ruleIDFilter string) ([]AlertNotification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]AlertNotification, 0)
	for _, n := range s.notifications {
		if ruleIDFilter != "" && n.RuleID != ruleIDFilter {
			continue
		}
		result = append(result, n)
	}
	// Return newest first.
	sort.Slice(result, func(i, j int) bool {
		return result[i].SentAt.After(result[j].SentAt)
	})
	return result, nil
}

// saveLocked saves without acquiring the lock (caller must hold s.mu).
func (s *RuleStore) saveLocked() error {
	rules := make([]Rule, 0, len(s.rules))
	for _, r := range s.rules {
		rules = append(rules, r)
	}
	states := make([]AlertState, 0, len(s.states))
	for _, st := range s.states {
		states = append(states, st)
	}

	pd := persistedData{
		Rules:         rules,
		States:        states,
		Notifications: s.notifications,
	}
	data, err := json.MarshalIndent(pd, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal store: %w", err)
	}
	return os.WriteFile(s.dbPath, data, 0644)
}
