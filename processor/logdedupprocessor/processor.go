// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logdedupprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/logdedupprocessor"

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
)

// logDedupProcessor is a logDedupProcessor that counts duplicate instances of logs.
type logDedupProcessor struct {
	emitInterval time.Duration
	aggregator   *logAggregator
	remover      *fieldRemover
	consumer     consumer.Logs
	logger       *zap.Logger
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mux          sync.Mutex
}

func newProcessor(cfg *Config, consumer consumer.Logs, logger *zap.Logger) (*logDedupProcessor, error) {
	// This should not happen due to config validation but we check anyways.
	timezone, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}

	return &logDedupProcessor{
		emitInterval: cfg.Interval,
		aggregator:   newLogAggregator(cfg.LogCountAttribute, timezone),
		remover:      newFieldRemover(cfg.ExcludeFields),
		consumer:     consumer,
		logger:       logger,
	}, nil
}

// Start starts the processor.
func (p *logDedupProcessor) Start(ctx context.Context, _ component.Host) error {
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	p.wg.Add(1)
	go p.handleExportInterval(ctx)

	return nil
}

// Capabilities returns the consumer's capabilities.
func (p *logDedupProcessor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}

// Shutdown stops the processor.
func (p *logDedupProcessor) Shutdown(_ context.Context) error {
	if p.cancel != nil {
		// Call cancel to stop the export interval goroutine and wait for it to finish.
		p.cancel()
		p.wg.Wait()
	}
	return nil
}

// ConsumeLogs processes the logs.
func (p *logDedupProcessor) ConsumeLogs(_ context.Context, pl plog.Logs) error {
	p.mux.Lock()
	defer p.mux.Unlock()

	for i := 0; i < pl.ResourceLogs().Len(); i++ {
		rl := pl.ResourceLogs().At(i)
		resource := rl.Resource()

		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			sl := rl.ScopeLogs().At(j)
			scope := sl.Scope()

			for k := 0; k < sl.LogRecords().Len(); k++ {
				logRecord := sl.LogRecords().At(k)
				// Remove excluded fields if any
				p.remover.RemoveFields(logRecord)

				// Add the log to the aggregator
				p.aggregator.Add(resource, scope, logRecord)
			}
		}
	}

	return nil
}

// handleExportInterval sends metrics at the configured interval.
func (p *logDedupProcessor) handleExportInterval(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.emitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Export any remaining logs
			p.exportLogs(ctx)
			if err := ctx.Err(); err != context.Canceled {
				p.logger.Error("context error", zap.Error(err))
			}
			return
		case <-ticker.C:
			p.exportLogs(ctx)
		}
	}
}

// exportLogs exports the logs to the next consumer.
func (p *logDedupProcessor) exportLogs(ctx context.Context) {
	p.mux.Lock()
	defer p.mux.Unlock()

	logs := p.aggregator.Export()
	// Only send logs if we have some
	if logs.LogRecordCount() > 0 {
		err := p.consumer.ConsumeLogs(ctx, logs)
		if err != nil {
			p.logger.Error("failed to consume logs", zap.Error(err))
		}
	}
	p.aggregator.Reset()
}