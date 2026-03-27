package publishqueue

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"sync"
)

type Publisher interface {
	Publish(ctx context.Context, atDID string, content map[string]interface{}) (string, error)
}

type WorkerPublisher struct {
	logger   *log.Logger
	delegate Publisher

	workers int
	lanes   []chan publishRequest

	closeOnce sync.Once
	wg        sync.WaitGroup
}

type publishRequest struct {
	ctx     context.Context
	atDID   string
	content map[string]interface{}
	resp    chan publishResponse
}

type publishResponse struct {
	ref string
	err error
}

func New(delegate Publisher, workers int, logger *log.Logger) *WorkerPublisher {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	if workers <= 0 {
		workers = 1
	}

	wp := &WorkerPublisher{
		logger:   logger,
		delegate: delegate,
		workers:  workers,
		lanes:    make([]chan publishRequest, workers),
	}

	for i := 0; i < workers; i++ {
		wp.lanes[i] = make(chan publishRequest, 256)
		wp.wg.Add(1)
		go wp.runLane(i, wp.lanes[i])
	}

	return wp
}

func (p *WorkerPublisher) Publish(ctx context.Context, atDID string, content map[string]interface{}) (string, error) {
	if p == nil || p.delegate == nil {
		return "", fmt.Errorf("worker publisher delegate is nil")
	}

	idx := p.laneIndex(atDID)
	req := publishRequest{
		ctx:     ctx,
		atDID:   atDID,
		content: content,
		resp:    make(chan publishResponse, 1),
	}

	select {
	case p.lanes[idx] <- req:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	select {
	case res := <-req.resp:
		return res.ref, res.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (p *WorkerPublisher) Close() error {
	if p == nil {
		return nil
	}

	p.closeOnce.Do(func() {
		for _, lane := range p.lanes {
			close(lane)
		}
		p.wg.Wait()
	})

	return nil
}

func (p *WorkerPublisher) laneIndex(atDID string) int {
	if p.workers <= 1 {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(atDID))
	return int(hasher.Sum32() % uint32(p.workers))
}

func (p *WorkerPublisher) runLane(idx int, lane <-chan publishRequest) {
	defer p.wg.Done()
	for req := range lane {
		ref, err := p.delegate.Publish(req.ctx, req.atDID, req.content)
		req.resp <- publishResponse{ref: ref, err: err}
	}
	p.logger.Printf("event=publish_lane_stopped lane=%d", idx)
}
