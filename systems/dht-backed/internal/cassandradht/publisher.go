package cassandradht

import (
	contract "canonicalingestion"
	"context"
	"errors"
	"sync"
	"time"

	"dhtbacked/internal/hashing"
)

type ProofWriter interface {
	PutProof(context.Context, hashing.Digest, []byte) error
}
type publication struct {
	key   hashing.Digest
	value []byte
}

// Publisher overlaps construction with immutable proof writes. Its byte and
// request bounds include queued AND in-flight payloads. Flush is a durable
// acknowledgement barrier, not merely a queue handoff. Ingestion never fills
// the query cache. Workers exist only between the first Put and Flush/Close.
type Publisher struct {
	ctx     context.Context
	store   ProofWriter
	limits  Limits
	Metrics *contract.IngestMetrics // Set before the first Put.

	apiMu     sync.Mutex // Serialize Put, Flush and cycle teardown.
	mu        sync.Mutex
	changed   chan struct{}
	jobs      chan publication
	runCtx    context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	bytes     int
	count     int
	first     error
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

func NewPublisher(ctx context.Context, store ProofWriter, l Limits) (*Publisher, error) {
	if ctx == nil || store == nil {
		return nil, errors.New("persistent Cassandra publisher and context required")
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &Publisher{ctx: ctx, store: store, limits: l, changed: make(chan struct{})}, nil
}

func (p *Publisher) signalLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}
func (p *Publisher) failureLocked() error {
	if p.first != nil {
		return p.first
	}
	if p.closed {
		return errors.New("cassandradht: publisher closed")
	}
	return p.ctx.Err()
}
func (p *Publisher) releaseLocked(item publication) {
	p.bytes -= len(item.value) + 256
	p.count--
	p.Metrics.TrackPendingBatchBytes(int64(p.bytes))
	p.Metrics.TrackStorageQueue(int64(p.count), int64(p.bytes))
	p.signalLocked()
}

func (p *Publisher) startLocked() {
	p.runCtx, p.cancel = context.WithCancel(p.ctx)
	// A small channel bounds queue metadata too; payload reservations below
	// impose the configured byte/request limits even while a send is blocked.
	p.jobs = make(chan publication, min(p.limits.MaxRequests, p.limits.Workers*2, max(1, p.limits.QueueBytes/257)))
	for range min(p.limits.Workers, p.limits.MaxRequests, p.limits.QueueBytes/257) {
		p.wg.Add(1)
		go p.worker(p.runCtx, p.jobs)
	}
}
func (p *Publisher) worker(ctx context.Context, jobs <-chan publication) {
	defer p.wg.Done()
	for {
		var item publication
		select {
		case <-ctx.Done():
			return
		case next, ok := <-jobs:
			if !ok {
				return
			}
			item = next
		}
		err := ctx.Err()
		if err == nil {
			started := time.Now()
			if p.Metrics != nil {
				p.Metrics.StorageStarted()
			}
			err = p.store.PutProof(ctx, item.key, item.value)
			if p.Metrics != nil {
				p.Metrics.StorageFinished(time.Since(started), err)
			}
		}
		p.mu.Lock()
		if err != nil && p.first == nil {
			p.first = err
			p.cancel()
		}
		p.releaseLocked(item)
		p.mu.Unlock()
	}
}

func (p *Publisher) Put(key hashing.Digest, value []byte) error {
	cost := len(value) + 256
	if len(value) == 0 || len(value) > p.limits.MaxValueBytes || cost > p.limits.QueueBytes {
		return errors.New("cassandradht: publication value/queue bound exceeded")
	}
	p.apiMu.Lock()
	defer p.apiMu.Unlock()
	p.mu.Lock()
	for {
		if err := p.failureLocked(); err != nil {
			p.mu.Unlock()
			return err
		}
		if p.jobs == nil {
			p.startLocked()
		}
		if p.count < p.limits.MaxRequests && cost <= p.limits.QueueBytes-p.bytes {
			break
		}
		changed, run := p.changed, p.runCtx
		p.mu.Unlock()
		select {
		case <-changed:
		case <-run.Done():
		}
		p.mu.Lock()
	}
	p.bytes += cost
	p.count++
	p.Metrics.TrackPendingBatchBytes(int64(p.bytes))
	p.Metrics.TrackStorageQueue(int64(p.count), int64(p.bytes))
	item := publication{key, append([]byte(nil), value...)}
	jobs, run := p.jobs, p.runCtx
	p.mu.Unlock()
	select {
	case jobs <- item:
		return nil // Only Flush can acknowledge the entire epoch.
	case <-run.Done():
		p.mu.Lock()
		p.releaseLocked(item)
		err := p.failureLocked()
		p.mu.Unlock()
		if err != nil {
			return err
		}
		return run.Err()
	}
}

// finishCycle requires apiMu and joins every worker before releasing buffers.
func (p *Publisher) finishCycle() error {
	p.mu.Lock()
	jobs := p.jobs
	if jobs != nil {
		close(jobs)
	}
	p.mu.Unlock()
	p.wg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	if jobs != nil {
		for item := range jobs {
			p.releaseLocked(item) // Cancelled jobs that no worker consumed.
		}
		p.cancel()
	}
	p.jobs, p.runCtx, p.cancel = nil, nil, nil
	p.signalLocked()
	if p.first != nil {
		return p.first
	}
	return p.ctx.Err()
}
func (p *Publisher) Flush() error {
	p.apiMu.Lock()
	defer p.apiMu.Unlock()
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return errors.New("cassandradht: publisher closed")
	}
	return p.finishCycle()
}

// Close aborts unfinished publication on an early construction/SQL error. It
// also wakes a producer blocked by backpressure, then joins all storage calls.
func (p *Publisher) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		if p.cancel != nil {
			p.cancel()
		}
		p.signalLocked()
		p.mu.Unlock()
		p.apiMu.Lock()
		defer p.apiMu.Unlock()
		p.closeErr = p.finishCycle()
	})
	return p.closeErr
}
