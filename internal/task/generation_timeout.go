package task

import (
	"context"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/service/generation"
	"go.uber.org/zap"
)

type GenerationTimeoutProcessor struct {
	generationService *generation.Service
	interval          time.Duration
	stopCh            chan struct{}
}

func NewGenerationTimeoutProcessor(generationService *generation.Service) *GenerationTimeoutProcessor {
	return &GenerationTimeoutProcessor{
		generationService: generationService,
		interval:          5 * time.Minute,
		stopCh:            make(chan struct{}),
	}
}

func (p *GenerationTimeoutProcessor) Start() {
	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ctx := context.Background()
				if err := p.generationService.ExpireTimeoutGenerations(ctx); err != nil {
					zap.L().Error("generation timeout processor error", zap.Error(err))
				}
			case <-p.stopCh:
				return
			}
		}
	}()
	zap.L().Info("generation timeout processor started", zap.Duration("interval", p.interval))
}

func (p *GenerationTimeoutProcessor) Stop() {
	close(p.stopCh)
}
