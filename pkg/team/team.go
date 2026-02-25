package team

import (
	"sync"
	"time"
)

// Team represents a coordinated group of agents.
type Team struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	LeaderID string   `json:"leader_id"`
	Members  []Member `json:"members"`
}

// Member represents a single agent within a team.
type Member struct {
	AgentID  string `json:"agent_id"`
	Role     string `json:"role"`
	Strategy string `json:"strategy,omitempty"`
}

// StatusReport is a snapshot of a member's current state.
type StatusReport struct {
	AgentID       string    `json:"agent_id"`
	System        string    `json:"system"`
	POI           string    `json:"poi"`
	CurrentAction string    `json:"current_action"`
	Docked        bool      `json:"docked"`
	InCombat      bool      `json:"in_combat"`
	Hull          float64   `json:"hull"`
	Fuel          float64   `json:"fuel"`
	Credits       float64   `json:"credits"`
	CargoUsed     float64   `json:"cargo_used"`
	CargoCapacity float64   `json:"cargo_capacity"`
	Timestamp     time.Time `json:"timestamp"`
}

// Order represents a command issued by a team leader.
type Order struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // "move", "attack", "dock", "mine", "regroup"
	Target    string    `json:"target"`
	Priority  int       `json:"priority"`
	IssuedBy  string    `json:"issued_by"`
	ExpiresAt time.Time `json:"expires_at"`
}

// OrderQueue is a thread-safe queue of orders for a team member.
type OrderQueue struct {
	orders []Order
	mu     sync.Mutex
}

// NewOrderQueue creates an empty order queue.
func NewOrderQueue() *OrderQueue {
	return &OrderQueue{}
}

// Push adds an order to the queue.
func (q *OrderQueue) Push(o Order) {
	q.mu.Lock()
	q.orders = append(q.orders, o)
	q.mu.Unlock()
}

// Pop returns and removes the highest priority pending order, or nil if empty.
func (q *OrderQueue) Pop() *Order {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.orders) == 0 {
		return nil
	}

	// Remove expired orders first.
	now := time.Now()
	valid := q.orders[:0]
	for _, o := range q.orders {
		if o.ExpiresAt.IsZero() || o.ExpiresAt.After(now) {
			valid = append(valid, o)
		}
	}
	q.orders = valid

	if len(q.orders) == 0 {
		return nil
	}

	// Find highest priority.
	best := 0
	for i, o := range q.orders {
		if o.Priority > q.orders[best].Priority {
			best = i
		}
	}

	order := q.orders[best]
	q.orders = append(q.orders[:best], q.orders[best+1:]...)
	return &order
}

// Len returns the number of pending orders.
func (q *OrderQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.orders)
}
