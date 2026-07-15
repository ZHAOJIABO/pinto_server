package task

import (
	"context"
	"time"

	ai_generation "github.com/zhaojiabo/bobobeads_server/internal/service/ai_generation"
	"go.uber.org/zap"
)

type AIGenerationProcessor struct {
	aiService *ai_generation.Service
	interval  time.Duration
	stopCh    chan struct{}
}

func NewAIGenerationProcessor(aiService *ai_generation.Service) *AIGenerationProcessor {
	return &AIGenerationProcessor{
		aiService: aiService,
		interval:  30 * time.Second,
		stopCh:    make(chan struct{}),
	}
}

func (p *AIGenerationProcessor) Start() {
	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ctx := context.Background()
				if err := p.aiService.ProcessPendingTasks(ctx); err != nil {
					zap.L().Error("ai generation processor error", zap.Error(err))
				}
			case <-p.stopCh:
				return
			}
		}
	}()
	zap.L().Info("ai generation processor started", zap.Duration("interval", p.interval))
}

func (p *AIGenerationProcessor) Stop() {
	close(p.stopCh)
}
