package guardian

import (
	"errors"
	"net/url"
	"sync"
	"time"

	"github.com/zhaowenjie/llm-serving-guardian/internal/config"
)

// Worker is an immutable upstream target selected by the pool.
type Worker struct {
	Name    string
	baseURL *url.URL
	apiKey  string
}

// Snapshot is the safe, read-only worker representation returned by the admin API.
type Snapshot struct {
	Name                string    `json:"name"`
	URL                 string    `json:"url"`
	IsHealthy           bool      `json:"isHealthy"`
	CircuitState        string    `json:"circuitState"`
	ConsecutiveFailures int       `json:"consecutiveFailures"`
	LastCheck           time.Time `json:"lastCheck,omitempty"`
	LastError           string    `json:"lastError,omitempty"`
	CircuitOpenUntil    time.Time `json:"circuitOpenUntil,omitempty"`
}

type workerState struct {
	worker              *Worker
	healthy             bool
	consecutiveFailures int
	lastCheck           time.Time
	lastError           string
	circuitOpenUntil    time.Time
}

// Pool owns routing and circuit state for all configured workers.
type Pool struct {
	mu               sync.RWMutex
	workers          []*workerState
	next             int
	failureThreshold int
	cooldown         time.Duration
	now              func() time.Time
}

func NewPool(workers []config.Worker, failureThreshold int, cooldown time.Duration) (*Pool, error) {
	if len(workers) == 0 {
		return nil, errors.New("worker pool requires at least one worker")
	}
	if failureThreshold <= 0 || cooldown <= 0 {
		return nil, errors.New("worker pool circuit settings must be positive")
	}

	pool := &Pool{failureThreshold: failureThreshold, cooldown: cooldown, now: time.Now}
	seen := make(map[string]struct{}, len(workers))
	for _, cfg := range workers {
		if _, exists := seen[cfg.Name]; exists {
			return nil, errors.New("worker names must be unique")
		}
		seen[cfg.Name] = struct{}{}
		baseURL, err := url.Parse(cfg.URL)
		if err != nil {
			return nil, errors.New("parse worker URL")
		}
		pool.workers = append(pool.workers, &workerState{worker: &Worker{
			Name: cfg.Name, baseURL: baseURL, apiKey: cfg.APIKey,
		}})
	}
	return pool, nil
}

// Next returns the next eligible worker, excluding workers already attempted.
func (p *Pool) Next(exclude map[string]bool) (*Worker, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for offset := range len(p.workers) {
		index := (p.next + offset) % len(p.workers)
		state := p.workers[index]
		if !state.healthy || !state.circuitOpenUntil.IsZero() || exclude[state.worker.Name] {
			continue
		}
		p.next = (index + 1) % len(p.workers)
		return state.worker, true
	}
	return nil, false
}

// ProbeCandidates returns workers whose circuit does not currently prevent a probe.
func (p *Pool) ProbeCandidates() []*Worker {
	p.mu.RLock()
	defer p.mu.RUnlock()

	now := p.now()
	workers := make([]*Worker, 0, len(p.workers))
	for _, state := range p.workers {
		if state.circuitOpenUntil.IsZero() || !now.Before(state.circuitOpenUntil) {
			workers = append(workers, state.worker)
		}
	}
	return workers
}

// ReportSuccess closes the circuit and makes a worker eligible.
func (p *Pool) ReportSuccess(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if state := p.find(name); state != nil {
		state.healthy = true
		state.consecutiveFailures = 0
		state.lastCheck = p.now()
		state.lastError = ""
		state.circuitOpenUntil = time.Time{}
	}
}

// ReportFailure records a passive or active failure and opens the circuit at the threshold.
func (p *Pool) ReportFailure(name string, failure error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if state := p.find(name); state != nil {
		state.consecutiveFailures++
		state.lastCheck = p.now()
		if failure != nil {
			state.lastError = failure.Error()
		}
		if state.consecutiveFailures >= p.failureThreshold {
			state.healthy = false
			state.circuitOpenUntil = p.now().Add(p.cooldown)
		}
	}
}

func (p *Pool) HealthyCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, state := range p.workers {
		if state.healthy && state.circuitOpenUntil.IsZero() {
			count++
		}
	}
	return count
}

func (p *Pool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.workers)
}

func (p *Pool) Snapshot() []Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	now := p.now()
	result := make([]Snapshot, 0, len(p.workers))
	for _, state := range p.workers {
		circuitState := "CLOSED"
		if !state.circuitOpenUntil.IsZero() && now.Before(state.circuitOpenUntil) {
			circuitState = "OPEN"
		} else if !state.healthy {
			circuitState = "PROBING"
		}
		result = append(result, Snapshot{
			Name: state.worker.Name, URL: state.worker.baseURL.String(), IsHealthy: state.healthy,
			CircuitState: circuitState, ConsecutiveFailures: state.consecutiveFailures,
			LastCheck: state.lastCheck, LastError: state.lastError, CircuitOpenUntil: state.circuitOpenUntil,
		})
	}
	return result
}

// find must be called with p.mu held.
func (p *Pool) find(name string) *workerState {
	for _, state := range p.workers {
		if state.worker.Name == name {
			return state
		}
	}
	return nil
}
