package transparencyprocessor

import (
	"context"
	"github.com/akkbng/otel-transparency-collector/internal/filter/expr"
	"github.com/akkbng/otel-transparency-collector/internal/idbatcher"
	"github.com/akkbng/otel-transparency-collector/internal/sampling"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlspan"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akkbng/otel-transparency-collector/internal/coreinternal/timeutils"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

var processorCapabilities = consumer.Capabilities{MutatesData: true}

// this is gonna be deleted after we get the services list from tilt file
var serviceList = [4]string{"cartservice", "emailservice", "quoteservice", "paymentservice"}

const (
	attrCheckFlag           = "tilt.check_flag"
	attrCategories          = "tilt.dataDisclosed.category"
	attrLegalBases          = "tilt.legal_bases"
	attrLegitimateInterests = "tilt.legitimate_interests"
	attrStorages            = "tilt.storage_durations"
	attrPurposes            = "tilt.purposes"
	attrAutomatedDecision   = "tilt.automated_decision_making"
)

type tiltAttributes struct {
	lastUpdated         time.Time
	checkFlag           bool
	categories          []string
	legalBases          []string
	legitimateInterests []bool
	storages            []string
	purposes            []string
	automatedDecision   bool
	serviceName         string
}

// policy combines a sampling policy evaluator with the destinations to be
// used for that policy.
type policy struct {
	// name used to identify this policy instance.
	name string
	// evaluator that decides if a trace is sampled or not by this policy instance.
	evaluator sampling.PolicyEvaluator
	// ctx used to carry metric tags of each policy.
	ctx context.Context
}

type transparencyProcessor struct {
	logger *zap.Logger
	ctx    context.Context

	timeout time.Duration

	mu              sync.RWMutex
	attributesCache map[string]tiltAttributes
	skipExpr        expr.BoolExpr[ottlspan.TransformContext]

	nextConsumer    consumer.Traces
	maxNumTraces    uint64
	policies        []*policy
	idToTrace       sync.Map
	policyTicker    timeutils.TTicker
	tickerFrequency time.Duration
	decisionBatcher idbatcher.Batcher
	deleteChan      chan pcommon.TraceID
	numTracesOnMap  *atomic.Uint64
}

func newTransparencyProcessor(ctx context.Context, settings component.TelemetrySettings, logger *zap.Logger, skipExpr expr.BoolExpr[ottlspan.TransformContext], nextConsumer consumer.Traces, cfg Config) (*transparencyProcessor, error) {
	if nextConsumer == nil {
		return nil, component.ErrNilNextConsumer
	}
	numDecisionBatches := uint64(cfg.DecisionWait.Seconds())
	inBatcher, err := idbatcher.New(numDecisionBatches, cfg.ExpectedNewTracesPerSec, uint64(2*runtime.NumCPU()))
	if err != nil {
		return nil, err
	}

	var policies []*policy
	for i := range cfg.PolicyCfgs {
		policyCfg := &cfg.PolicyCfgs[i]
		policyCtx, err := tag.New(ctx, tag.Upsert(tagPolicyKey, policyCfg.Name), tag.Upsert(tagSourceFormat, "transparency_tail_sampling"))
		if err != nil {
			return nil, err
		}
		eval, err := getPolicyEvaluator(settings, policyCfg)
		if err != nil {
			return nil, err
		}
		p := &policy{
			name:      policyCfg.Name,
			evaluator: eval,
			ctx:       policyCtx,
		}
		policies = append(policies, p)
	}

	tsp := &transparencyProcessor{
		logger:          logger,
		nextConsumer:    nextConsumer,
		skipExpr:        skipExpr,
		ctx:             ctx,
		maxNumTraces:    cfg.NumTraces,
		decisionBatcher: inBatcher,
		policies:        policies,
		tickerFrequency: time.Second,
		numTracesOnMap:  &atomic.Uint64{},
	}

	tsp.policyTicker = &timeutils.PolicyTicker{OnTickFunc: tsp.samplingPolicyOnTick}
	tsp.deleteChan = make(chan pcommon.TraceID, cfg.NumTraces)

	return tsp, nil
}

func getPolicyEvaluator(settings component.TelemetrySettings, cfg *PolicyCfg) (sampling.PolicyEvaluator, error) {
	switch cfg.Type {
	case Composite:
		return getNewCompositePolicy(settings, &cfg.CompositeCfg)
	case And:
		return getNewAndPolicy(settings, &cfg.AndCfg)
	default:
		return getSharedPolicyEvaluator(settings, &cfg.sharedPolicyCfg)
	}
}

// ConsumeTraces is required by the processor.Traces interface.
func (tsp *transparencyProcessor) ConsumeTraces(ctx context.Context, td ptrace.Traces) error {
	resourceSpans := td.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		tsp.processTraces(ctx, resourceSpans.At(i))
	}
	return nil
}

func (tsp *transparencyProcessor) groupSpansByTraceKey(ctx context.Context, resourceSpans ptrace.ResourceSpans) map[pcommon.TraceID][]*ptrace.Span {
	idToSpans := make(map[pcommon.TraceID][]*ptrace.Span)
	ilss := resourceSpans.ScopeSpans()
	for j := 0; j < ilss.Len(); j++ {
		spans := ilss.At(j).Spans()
		spansLen := spans.Len()
		for k := 0; k < spansLen; k++ {
			span := spans.At(k)
			if tsp.skipExpr != nil {
				skip, err := tsp.skipExpr.Eval(ctx, ottlspan.NewTransformContext(span, ilss.At(j).Scope(), resourceSpans.Resource()))
				if err != nil {
					return nil //TODO: return error, check error handling
				}
				if skip {
					continue
				}
			}
			tiltComponent, ok := span.Attributes().Get(attrCategories)
			if !ok {
				continue
			}
			tsp.insertTiltCheck(&span, tiltComponent)
			key := span.TraceID()
			idToSpans[key] = append(idToSpans[key], &span)
		}
	}
	return idToSpans
}

func (tsp *transparencyProcessor) processTraces(ctx context.Context, resourceSpans ptrace.ResourceSpans) {
	// Group spans per their traceId to minimize contention on idToTrace
	idToSpans := tsp.groupSpansByTraceKey(ctx, resourceSpans)
	var newTraceIDs int64
	for id, spans := range idToSpans {
		lenSpans := int64(len(spans))
		lenPolicies := len(tsp.policies)
		initialDecisions := make([]sampling.Decision, lenPolicies)
		for i := 0; i < lenPolicies; i++ {
			initialDecisions[i] = sampling.Pending
		}
		d, loaded := tsp.idToTrace.Load(id)
		if !loaded {
			spanCount := &atomic.Int64{}
			spanCount.Store(lenSpans)
			d, loaded = tsp.idToTrace.LoadOrStore(id, &sampling.TraceData{
				Decisions:       initialDecisions,
				ArrivalTime:     time.Now(),
				SpanCount:       spanCount,
				ReceivedBatches: ptrace.NewTraces(),
			})
		}
		actualData := d.(*sampling.TraceData)
		if loaded {
			actualData.SpanCount.Add(lenSpans)
		} else {
			newTraceIDs++
			tsp.decisionBatcher.AddToCurrentBatch(id)
			tsp.numTracesOnMap.Add(1)
			postDeletion := false
			currTime := time.Now()
			for !postDeletion {
				select {
				case tsp.deleteChan <- id:
					postDeletion = true
				default:
					traceKeyToDrop := <-tsp.deleteChan
					tsp.dropTrace(traceKeyToDrop, currTime)
				}
			}
		}

		// The only thing we really care about here is the final decision.
		actualData.Lock()
		finalDecision := actualData.FinalDecision

		if finalDecision == sampling.Unspecified {
			// If the final decision hasn't been made, add the new spans under the lock.
			appendToTraces(actualData.ReceivedBatches, resourceSpans, spans)
			actualData.Unlock()
		} else {
			actualData.Unlock()

			switch finalDecision {
			case sampling.Sampled:
				// Forward the spans to the policy destinations
				traceTd := ptrace.NewTraces()
				appendToTraces(traceTd, resourceSpans, spans)
				if err := tsp.nextConsumer.ConsumeTraces(tsp.ctx, traceTd); err != nil {
					tsp.logger.Warn(
						"Error sending late arrived spans to destination",
						zap.Error(err))
				}
			case sampling.NotSampled:
				stats.Record(tsp.ctx, statLateSpanArrivalAfterDecision.M(int64(time.Since(actualData.DecisionTime)/time.Second)))
			default:
				tsp.logger.Warn("Encountered unexpected sampling decision",
					zap.Int("decision", int(finalDecision)))
			}
		}
	}
	stats.Record(tsp.ctx, statNewTraceIDReceivedCount.M(newTraceIDs))
}

func (tsp *transparencyProcessor) insertTiltCheck(span *ptrace.Span, tiltComponent pcommon.Value) {
	//if tiltComponent value is not empty, add "true" as the value of the checkFlag attribute
	if tiltComponent.AsString() != "" {
		span.Attributes().PutBool(attrCheckFlag, true)
	} else {
		span.Attributes().PutBool(attrCheckFlag, false)
	}
}

func (tsp *transparencyProcessor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

// Start is invoked during service startup.
func (tsp *transparencyProcessor) Start(context.Context, component.Host) error {
	tsp.policyTicker.Start(tsp.tickerFrequency)
	return nil
}

// Shutdown is invoked during service shutdown.
func (tsp *transparencyProcessor) Shutdown(context.Context) error {
	tsp.decisionBatcher.Stop()
	tsp.policyTicker.Stop()
	return nil
}

func (tsp *transparencyProcessor) dropTrace(traceID pcommon.TraceID, deletionTime time.Time) {
	var trace *sampling.TraceData
	if d, ok := tsp.idToTrace.Load(traceID); ok {
		trace = d.(*sampling.TraceData)
		tsp.idToTrace.Delete(traceID)
		// Subtract one from numTracesOnMap per https://godoc.org/sync/atomic#AddUint64
		tsp.numTracesOnMap.Add(^uint64(0))
	}
	if trace == nil {
		tsp.logger.Error("Attempt to delete traceID not on table")
		return
	}

	stats.Record(tsp.ctx, statTraceRemovalAgeSec.M(int64(deletionTime.Sub(trace.ArrivalTime)/time.Second)))
}

func appendToTraces(dest ptrace.Traces, rss ptrace.ResourceSpans, spans []*ptrace.Span) {
	rs := dest.ResourceSpans().AppendEmpty()
	rss.Resource().CopyTo(rs.Resource())
	ils := rs.ScopeSpans().AppendEmpty()
	for _, span := range spans {
		sp := ils.Spans().AppendEmpty()
		span.CopyTo(sp)
	}
}
