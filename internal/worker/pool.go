// Package worker provides an async message-processing pool.
//
// The webhook handler enqueues inbound messages into a buffered channel and
// returns HTTP 200 immediately (as Meta requires). A fixed pool of worker
// goroutines drains the channel and calls the bot handler.
//
// This completely decouples webhook latency from DB / WhatsApp API latency,
// allowing the server to sustain thousands of concurrent conversations.
//
// CRITICAL: If a message cannot be queued after retries, it is LOST.
// Monitor queue length and dropped message rate closely.
package worker

import (
	"log/slog"
	"time"

	"github.com/ataljanseva/janseva-chatbot/internal/whatsapp"
)

// Job is a single unit of work: one inbound WhatsApp message.
type Job struct {
	Msg whatsapp.Message
}

// Handler is the function signature the bot must implement.
type Handler func(msg whatsapp.Message)

// Pool manages a fixed set of goroutines that drain a shared job queue.
type Pool struct {
	queue   chan Job
	handler Handler
	workers int
	dropped int64 // Atomic counter of dropped messages
}

// New creates a Pool with `workers` goroutines and a buffered queue of
// `queueDepth` jobs. Call Start() to launch the goroutines.
func New(workers, queueDepth int, handler Handler) *Pool {
	return &Pool{
		queue:   make(chan Job, queueDepth),
		handler: handler,
		workers: workers,
		dropped: 0,
	}
}

// Start launches the worker goroutines. It is non-blocking.
func (p *Pool) Start() {
	for i := 0; i < p.workers; i++ {
		go p.run(i)
	}
	slog.Info("worker pool started", "workers", p.workers, "queue_depth", cap(p.queue))
}

// Enqueue submits a job with exponential backoff retry.
// If queue remains full after retries, message is dropped and logged as ERROR.
// CRITICAL: Dropped messages are LOST and will NOT be retried by Meta.
func (p *Pool) Enqueue(msg whatsapp.Message) {
	// Try with backoff: 10ms, 20ms, 40ms (total 70ms)
	backoffs := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		40 * time.Millisecond,
	}

	// Attempt 1: Non-blocking attempt
	select {
	case p.queue <- Job{Msg: msg}:
		return // Success
	default:
	}

	// Attempts 2-4: Backoff retry
	for _, backoff := range backoffs {
		time.Sleep(backoff)
		select {
		case p.queue <- Job{Msg: msg}:
			slog.Debug("message queued after backoff", "from", msg.From, "backoff_ms", backoff.Milliseconds())
			return // Success
		default:
		}
	}

	// All attempts failed – message is LOST
	p.dropped++
	queueCap := cap(p.queue)
	queueLen := len(p.queue)
	slog.Error("MESSAGE DROPPED – queue full (all retries exhausted)",
		"from", msg.From,
		"type", msg.Type,
		"queue_len", queueLen,
		"queue_cap", queueCap,
		"total_dropped", p.dropped,
		"utilization", float64(queueLen)*100/float64(queueCap),
	)
}

// QueueLen returns the current number of pending jobs (useful for metrics).
func (p *Pool) QueueLen() int { return len(p.queue) }

// DroppedCount returns the total number of messages dropped due to queue overflow.
func (p *Pool) DroppedCount() int64 { return p.dropped }

func (p *Pool) run(id int) {
	slog.Debug("worker started", "id", id)
	for job := range p.queue {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("worker panic recovered",
						"worker_id", id,
						"from", job.Msg.From,
						"panic", r,
					)
				}
			}()
			p.handler(job.Msg)
		}()
	}
}
