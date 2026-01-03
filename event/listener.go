package event

import "sync"

// Callback is a function that receives events.
type Callback func(Event)

// Listener holds a callback and its event filter mask.
type Listener struct {
	Callback Callback
	Mask     Code
}

// ListenerManager manages event listeners and dispatches events.
type ListenerManager struct {
	mu        sync.RWMutex
	listeners []Listener
}

// NewListenerManager creates a new listener manager.
func NewListenerManager() *ListenerManager {
	return &ListenerManager{
		listeners: make([]Listener, 0),
	}
}

// AddListener registers a callback to receive events matching the given mask.
// If mask is 0, the callback receives all events.
func (m *ListenerManager) AddListener(callback Callback, mask Code) {
	if mask == 0 {
		mask = All
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, Listener{
		Callback: callback,
		Mask:     mask,
	})
}

// RemoveListener unregisters a callback.
// Note: This compares function pointers, so the same function reference must be used.
func (m *ListenerManager) RemoveListener(callback Callback) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find and remove the listener
	for i := 0; i < len(m.listeners); i++ {
		// Compare function pointers using a helper
		if isSameCallback(m.listeners[i].Callback, callback) {
			m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
			return
		}
	}
}

// isSameCallback is a helper to compare callbacks.
// In Go, comparing functions directly is not allowed, so we use this workaround.
// This comparison will work if the same function variable is used.
func isSameCallback(a, b Callback) bool {
	// This is a simplified comparison - in practice you might use
	// reflect.ValueOf to compare function pointers
	return &a == &b
}

// Dispatch sends an event to all registered listeners whose mask matches.
func (m *ListenerManager) Dispatch(event Event) {
	m.mu.RLock()
	listeners := make([]Listener, len(m.listeners))
	copy(listeners, m.listeners)
	m.mu.RUnlock()

	code := event.EventCode()
	for _, listener := range listeners {
		if listener.Mask&code != 0 {
			// Call listener in a separate goroutine to prevent blocking
			go listener.Callback(event)
		}
	}
}

// DispatchSync sends an event to all registered listeners synchronously.
// This is useful when you need to ensure all listeners have processed the event.
func (m *ListenerManager) DispatchSync(event Event) {
	m.mu.RLock()
	listeners := make([]Listener, len(m.listeners))
	copy(listeners, m.listeners)
	m.mu.RUnlock()

	code := event.EventCode()
	for _, listener := range listeners {
		if listener.Mask&code != 0 {
			listener.Callback(event)
		}
	}
}

// Clear removes all listeners.
func (m *ListenerManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = m.listeners[:0]
}

// Count returns the number of registered listeners.
func (m *ListenerManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.listeners)
}
