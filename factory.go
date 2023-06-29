package transparencyprocessor

import (
	"context"
	"github.com/akkbng/otel-transparency-collector/internal/filter/filterspan"
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
		DecisionWait:            30 * time.Second,
		NumTraces:               5000,
		ExpectedNewTracesPerSec: 1,
		PolicyCfgs: []PolicyCfg{
			{
				sharedPolicyCfg: sharedPolicyCfg{
					Name: "composite-policy-1",
					Type: Composite,
				},
				CompositeCfg: CompositeCfg{
					MaxTotalSpansPerSecond: 1000,
					PolicyOrder:            []string{"tilt-check-policy", "test-composite-policy-3"},
					SubPolicyCfg: []CompositeSubPolicyCfg{
						{
							sharedPolicyCfg: sharedPolicyCfg{
								Name:                "tilt-check-policy",
								Type:                BooleanAttribute,
								BooleanAttributeCfg: BooleanAttributeCfg{Key: "tilt.check_flag", Value: true},
							},
						},
						//{
						//	sharedPolicyCfg: sharedPolicyCfg{
						//		Name:               "trace-path-policy",
						//		Type:               StringAttribute,
						//		StringAttributeCfg: StringAttributeCfg{Key: "key2", Values: []string{"value1", "value2"}},
						//	},
						//},
						{
							sharedPolicyCfg: sharedPolicyCfg{
								Name: "test-composite-policy-3",
								Type: AlwaysSample,
							},
						},
					},
					RateAllocation: []RateAllocationCfg{
						{
							Policy:  "tilt-check-policy",
							Percent: 50,
						},
						//{
						//	Policy:  "trace-path-policy",
						//	Percent: 25,
						//},
					},
				},
			},
		},
	}
}

func createTracesProcessor(
	ctx context.Context,
	set processor.CreateSettings,
	cfg component.Config,
	nextConsumer consumer.Traces,
) (processor.Traces, error) {
	oCfg := cfg.(*Config)
	skipExpr, err := filterspan.NewSkipExpr(&oCfg.MatchConfig)
	if err != nil {
		return nil, err
	}
	return newTransparencyProcessor(ctx, set.TelemetrySettings, set.Logger, skipExpr, nextConsumer, *oCfg)
	//return processorhelper.NewTracesProcessor(
	//	ctx,
	//	set,
	//	cfg,
	//	nextConsumer,
	//	newTransparencyProcessor(set.Logger, skipExpr).processTraces,
	//	processorhelper.WithCapabilities(processorCapabilities))
}
