package sampling

import (
	"context"
	"github.com/akkbng/otel-transparency-collector/internal/filter/expr"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlspan"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"time"
)

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

type transparencyAttributeFilter struct {
	logger   *zap.Logger
	skipExpr expr.BoolExpr[ottlspan.TransformContext]
}

var _ PolicyEvaluator = (*transparencyAttributeFilter)(nil)

func NewTransparencyAttributeFilter(settings component.TelemetrySettings) PolicyEvaluator {
	return &transparencyAttributeFilter{
		logger: settings.Logger,
	}
}

// Evaluate looks at the trace data and returns a corresponding SamplingDecision.
func (taf *transparencyAttributeFilter) Evaluate(ctx context.Context, _ pcommon.TraceID, trace *TraceData) (Decision, error) {
	trace.Lock()
	batches := trace.ReceivedBatches
	trace.Unlock()

	for i := 0; i < batches.ResourceSpans().Len(); i++ {
		rs := batches.ResourceSpans().At(i)
		resource := rs.Resource()
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			scope := ss.Scope()
			for k := 0; k < ss.Spans().Len(); k++ {
				span := ss.Spans().At(k)

				var (
					skip bool
					err  error
				)

				// Now we reach span level and begin evaluation with parsed expr.
				// The evaluation will break when:
				// 1. error happened.
				// 2. "Sampled" decision made.
				// Otherwise, it will keep evaluating and finally exit with "NotSampled" decision.

				// Span evaluation
				if taf.skipExpr != nil {
					skip, err = taf.skipExpr.Eval(ctx, ottlspan.NewTransformContext(span, scope, resource))
					if err != nil {
						return Error, err
					}
					if skip {
						continue
					}
				}
				tiltComponent, ok := span.Attributes().Get(attrCategories)
				if !ok {
					continue
				}
				insertTiltCheck(span, tiltComponent)
				if insertTiltCheck(span, tiltComponent) == true {
					return Sampled, nil
				}
			}
		}
	}
	return NotSampled, nil
}

func insertTiltCheck(span ptrace.Span, tiltComponent pcommon.Value) bool {
	//if tiltComponent value is not empty, add "true" as the value of the checkFlag attribute
	if tiltComponent.AsString() != "" {
		span.Attributes().PutBool(attrCheckFlag, true)
		return true
	} else {
		span.Attributes().PutBool(attrCheckFlag, false)
		return false
	}
}
