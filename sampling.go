package transparencyprocessor

import (
	"github.com/akkbng/otel-transparency-collector/internal/sampling"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"time"
)

type policyMetrics struct {
	idNotFoundOnMapCount, evaluateErrorCount, decisionSampled, decisionNotSampled int64
}

func (tsp *transparencyProcessor) samplingPolicyOnTick() {
	metrics := policyMetrics{}

	startTime := time.Now()
	batch, _ := tsp.decisionBatcher.CloseCurrentAndTakeFirstBatch()
	batchLen := len(batch)
	tsp.logger.Debug("Sampling Policy Evaluation ticked")
	for _, id := range batch {
		d, ok := tsp.idToTrace.Load(id)
		if !ok {
			metrics.idNotFoundOnMapCount++
			continue
		}
		trace := d.(*sampling.TraceData)
		trace.DecisionTime = time.Now()

		decision, policy := tsp.makeDecision(id, trace, &metrics)

		// Sampled or not, remove the batches
		trace.Lock()
		allSpans := trace.ReceivedBatches
		trace.FinalDecision = decision
		trace.ReceivedBatches = ptrace.NewTraces()
		trace.Unlock()

		if decision == sampling.Sampled {
			_ = tsp.nextConsumer.ConsumeTraces(policy.ctx, allSpans)
		}
	}

	stats.Record(tsp.ctx,
		statOverallDecisionLatencyUs.M(int64(time.Since(startTime)/time.Microsecond)),
		statDroppedTooEarlyCount.M(metrics.idNotFoundOnMapCount),
		statPolicyEvaluationErrorCount.M(metrics.evaluateErrorCount),
		statTracesOnMemoryGauge.M(int64(tsp.numTracesOnMap.Load())))

	tsp.logger.Debug("Sampling policy evaluation completed",
		zap.Int("batch.len", batchLen),
		zap.Int64("sampled", metrics.decisionSampled),
		zap.Int64("notSampled", metrics.decisionNotSampled),
		zap.Int64("droppedPriorToEvaluation", metrics.idNotFoundOnMapCount),
		zap.Int64("policyEvaluationErrors", metrics.evaluateErrorCount),
	)
}

func (tsp *transparencyProcessor) makeDecision(id pcommon.TraceID, trace *sampling.TraceData, metrics *policyMetrics) (sampling.Decision, *policy) {
	finalDecision := sampling.NotSampled
	var matchingPolicy *policy
	samplingDecision := map[sampling.Decision]bool{
		sampling.Error:            false,
		sampling.Sampled:          false,
		sampling.NotSampled:       false,
		sampling.InvertSampled:    false,
		sampling.InvertNotSampled: false,
	}

	// Check all policies before making a final decision
	for i, p := range tsp.policies {
		policyEvaluateStartTime := time.Now()
		decision, err := p.evaluator.Evaluate(p.ctx, id, trace)
		stats.Record(
			p.ctx,
			statDecisionLatencyMicroSec.M(int64(time.Since(policyEvaluateStartTime)/time.Microsecond)))

		if err != nil {
			samplingDecision[sampling.Error] = true
			trace.Decisions[i] = sampling.NotSampled
			metrics.evaluateErrorCount++
			tsp.logger.Debug("Sampling policy error", zap.Error(err))
		} else {
			switch decision {
			case sampling.Sampled:
				samplingDecision[sampling.Sampled] = true
				trace.Decisions[i] = decision

			case sampling.NotSampled:
				samplingDecision[sampling.NotSampled] = true
				trace.Decisions[i] = decision

			case sampling.InvertSampled:
				samplingDecision[sampling.InvertSampled] = true
				trace.Decisions[i] = sampling.Sampled

			case sampling.InvertNotSampled:
				samplingDecision[sampling.InvertNotSampled] = true
				trace.Decisions[i] = sampling.NotSampled
			}
		}
	}

	// InvertNotSampled takes precedence over any other decision
	switch {
	case samplingDecision[sampling.InvertNotSampled]:
		finalDecision = sampling.NotSampled
	case samplingDecision[sampling.Sampled]:
		finalDecision = sampling.Sampled
	case samplingDecision[sampling.InvertSampled] && !samplingDecision[sampling.NotSampled]:
		finalDecision = sampling.Sampled
	}

	for _, p := range tsp.policies {
		switch finalDecision {
		case sampling.Sampled:
			// any single policy that decides to sample will cause the decision to be sampled
			// the nextConsumer will get the context from the first matching policy
			if matchingPolicy == nil {
				matchingPolicy = p
			}

			_ = stats.RecordWithTags(
				p.ctx,
				[]tag.Mutator{tag.Upsert(tagSampledKey, "true")},
				statCountTracesSampled.M(int64(1)),
			)
			metrics.decisionSampled++

		case sampling.NotSampled:
			_ = stats.RecordWithTags(
				p.ctx,
				[]tag.Mutator{tag.Upsert(tagSampledKey, "false")},
				statCountTracesSampled.M(int64(1)),
			)
			metrics.decisionNotSampled++
		}
	}

	return finalDecision, matchingPolicy
}
