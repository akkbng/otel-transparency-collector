package transparencyprocessor

import (
	"context"
	"go.opencensus.io/stats/view"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"sync"
	"time"

	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
)

var onceMetrics sync.Once

// NewFactory creates a factory for the transparency processor
func NewFactory() processor.Factory {
	onceMetrics.Do(func() {
		// TODO: this is hardcoding the metrics level and skips error handling
		_ = view.Register(SamplingProcessorMetricViews(configtelemetry.LevelNormal)...)
	})

	return processor.NewFactory(
		Type,
		createDefaultConfig,
		processor.WithTraces(createTracesProcessor, TracesStability))
}

func createDefaultConfig() component.Config {
	return &Config{
		DecisionWait: 30 * time.Second,
		NumTraces:    5000,
	}
}

func createTracesProcessor(
	ctx context.Context,
	params processor.CreateSettings,
	cfg component.Config,
	nextConsumer consumer.Traces,
) (processor.Traces, error) {
	tCfg := cfg.(*Config)
	return newTransparencyProcessor(ctx, params.TelemetrySettings, nextConsumer, *tCfg)
}
