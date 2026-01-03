package job

import (
	"fmt"
	"reflect"
	"runtime"
	"sync"
)

// FuncRegistry is a registry for job functions.
// It maps string references to actual Go functions.
// This is required for persistent job stores to restore functions from storage.
type FuncRegistry struct {
	mu    sync.RWMutex
	funcs map[string]interface{}
}

// globalRegistry is the default global function registry.
var globalRegistry = NewFuncRegistry()

// NewFuncRegistry creates a new function registry.
func NewFuncRegistry() *FuncRegistry {
	return &FuncRegistry{
		funcs: make(map[string]interface{}),
	}
}

// FuncRefForFunc returns the fully-qualified function name for use as a FuncRef.
func FuncRefForFunc(fn interface{}) (string, error) {
	if fn == nil {
		return "", fmt.Errorf("function cannot be nil")
	}

	val := reflect.ValueOf(fn)
	if val.Kind() != reflect.Func {
		return "", fmt.Errorf("value is not a function")
	}

	pc := val.Pointer()
	if pc == 0 {
		return "", fmt.Errorf("function pointer is zero")
	}

	fnInfo := runtime.FuncForPC(pc)
	if fnInfo == nil {
		return "", fmt.Errorf("function name could not be resolved")
	}

	return fnInfo.Name(), nil
}

// Register adds a function to the registry with the given reference name.
func (r *FuncRegistry) Register(ref string, fn interface{}) error {
	if ref == "" {
		return fmt.Errorf("function reference cannot be empty")
	}
	if fn == nil {
		return fmt.Errorf("function cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.funcs[ref]; exists {
		return fmt.Errorf("function reference %q already registered", ref)
	}

	r.funcs[ref] = fn
	return nil
}

// Lookup returns the function for the given reference.
func (r *FuncRegistry) Lookup(ref string) (interface{}, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.funcs[ref]
	return fn, ok
}

// Unregister removes a function from the registry.
func (r *FuncRegistry) Unregister(ref string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.funcs, ref)
}

// Clear removes all functions from the registry.
func (r *FuncRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.funcs = make(map[string]interface{})
}

// RegisterFunc registers a function in the global registry.
func RegisterFunc(ref string, fn interface{}) error {
	return globalRegistry.Register(ref, fn)
}

// RegisterFuncByName registers a function using its fully-qualified name.
// If already registered, it returns the existing reference.
func RegisterFuncByName(fn interface{}) (string, error) {
	ref, err := FuncRefForFunc(fn)
	if err != nil {
		return "", err
	}

	if _, ok := globalRegistry.Lookup(ref); ok {
		return ref, nil
	}

	if err := globalRegistry.Register(ref, fn); err != nil {
		return "", err
	}

	return ref, nil
}

// LookupFunc looks up a function in the global registry.
func LookupFunc(ref string) (interface{}, bool) {
	return globalRegistry.Lookup(ref)
}

// UnregisterFunc removes a function from the global registry.
func UnregisterFunc(ref string) {
	globalRegistry.Unregister(ref)
}

// ClearFuncRegistry clears the global function registry.
func ClearFuncRegistry() {
	globalRegistry.Clear()
}

// GetGlobalRegistry returns the global function registry.
func GetGlobalRegistry() *FuncRegistry {
	return globalRegistry
}
