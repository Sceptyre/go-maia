package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// ConcurrentConfig holds configuration for parallel tool execution.
type ConcurrentConfig struct {
	// MaxConcurrency limits the number of goroutines executing tools
	// simultaneously. Zero means unlimited (bounded only by len(toolCalls)).
	MaxConcurrency int
	// EnableLocking activates per-file mutex protection for write_file
	// calls that target the same path.
	EnableLocking bool
}

// DefaultConcurrentConfig returns sensible defaults.
func DefaultConcurrentConfig() ConcurrentConfig {
	return ConcurrentConfig{
		MaxConcurrency: 5,
		EnableLocking:  true,
	}
}

// ---------- File Lock Registry ----------

// FileLockRegistry manages per-file-path mutexes so that concurrent
// tool calls that write to the same file are serialised.
type FileLockRegistry struct {
	mu    sync.RWMutex
	locks map[string]*sync.Mutex
}

func NewFileLockRegistry() *FileLockRegistry {
	return &FileLockRegistry{locks: make(map[string]*sync.Mutex)}
}

func (r *FileLockRegistry) getOrCreate(filePath string) *sync.Mutex {
	r.mu.RLock()
	if l, ok := r.locks[filePath]; ok {
		r.mu.RUnlock()
		return l
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check after write-lock acquisition.
	if l, ok := r.locks[filePath]; ok {
		return l
	}
	l := &sync.Mutex{}
	r.locks[filePath] = l
	return l
}

func (r *FileLockRegistry) LockFile(filePath string)   { r.getOrCreate(filePath).Lock() }
func (r *FileLockRegistry) UnlockFile(filePath string) { r.getOrCreate(filePath).Unlock() }

// lockAwareToolHandler wraps a tool handler so that write_file calls
// acquire the file-level lock for their target path before execution.
func lockAwareToolHandler(
	handler func(ToolCall) (string, error),
	registry *FileLockRegistry,
) func(ToolCall) (string, error) {
	return func(call ToolCall) (string, error) {
		if call.Function.Name == "write_file" {
			var args map[string]string
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err == nil {
				if fp := args["path"]; fp != "" {
					registry.LockFile(fp)
					defer registry.UnlockFile(fp)
				}
			}
		}
		return handler(call)
	}
}

// ---------- Concurrent Executor ----------

// concurrentToolExecutor runs toolCalls in parallel using a bounded
// worker pool. Results are collected in an indexed slice so the original
// order is preserved regardless of goroutine scheduling. Each tool call
// is isolated: one failure does not prevent the others from completing.
func concurrentToolExecutor(
	ctx context.Context,
	toolCalls []ToolCall,
	toolHandler func(ToolCall) (string, error),
	config ConcurrentConfig,
) []Message {
	if len(toolCalls) == 0 {
		return nil
	}

	results := make([]Message, len(toolCalls))

	maxConcurrency := config.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = len(toolCalls)
	}
	sem := make(chan struct{}, maxConcurrency)

	var registry *FileLockRegistry
	if config.EnableLocking {
		registry = NewFileLockRegistry()
	}

	var wg sync.WaitGroup
	var mu sync.Mutex // protects the results slice

	for i, toolCall := range toolCalls {
		// Abort remaining launches if context is done.
		select {
		case <-ctx.Done():
			for j := i; j < len(toolCalls); j++ {
				results[j] = Message{
					Role:       "tool",
					Content:    strPtr(fmt.Sprintf("Error: %s", ctx.Err())),
					ToolCallID: toolCalls[j].ID,
				}
			}
			return results
		default:
		}

		wg.Add(1)
		go func(idx int, call ToolCall) {
			defer wg.Done()

			// Bounded parallelism via semaphore.
			sem <- struct{}{}
			defer func() { <-sem }()

			// Re-check context inside the goroutine.
			select {
			case <-ctx.Done():
				mu.Lock()
				results[idx] = Message{
					Role:       "tool",
					Content:    strPtr(fmt.Sprintf("Error: %s", ctx.Err())),
					ToolCallID: call.ID,
				}
				mu.Unlock()
				return
			default:
			}

			h := toolHandler
			if registry != nil {
				h = lockAwareToolHandler(toolHandler, registry)
			}

			result, err := h(call)
			if err != nil {
				result = fmt.Sprintf("Error: %s", err)
			}

			mu.Lock()
			results[idx] = Message{
				Role:       "tool",
				Content:    strPtr(result),
				ToolCallID: call.ID,
			}
			mu.Unlock()
		}(i, toolCall)
	}

	wg.Wait()
	return results
}
