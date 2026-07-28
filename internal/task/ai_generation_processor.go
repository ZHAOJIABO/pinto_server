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

func NewAIGenerationProcessor(aiService *ai_generation.Service, interval time.Duration) *AIGenerationProcessor {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &AIGenerationProcessor{
		aiService: aiService,
		interval:  interval,
		stopCh:    make(chan struct{}),
	}
}

func (p *AIGenerationProcessor) Start() {
	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		// Run once immediately so a restart recovers tasks abandoned by the
		// previous process without waiting a full interval.
		p.reap()

		for {
			select {
			case <-ticker.C:
				p.reap()
			case <-p.stopCh:
				return
			}
		}
	}()
	zap.L().Info("ai generation reaper started", zap.Duration("interval", p.interval))
}

func (p *AIGenerationProcessor) reap() {
	if err := p.aiService.ReapTasks(context.Background()); err != nil {
		zap.L().Error("ai generation reaper error", zap.Error(err))
	}
}

func (p *AIGenerationProcessor) Stop() {
	close(p.stopCh)
}
