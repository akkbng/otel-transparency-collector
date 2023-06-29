package sampling

import (
	"context"
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

func insertTiltCheck(span ptrace.Span) {
	tiltComponent, ok := span.Attributes().Get(attrCategories)
	if !ok {
		return
	}
	//if tiltComponent value is not empty, add "true" as the value of the checkFlag attribute
	if tiltComponent.AsString() != "" {
		span.Attributes().PutBool(attrCheckFlag, true)
	} else {
		span.Attributes().PutBool(attrCheckFlag, false)
	}
}

type transparencyAttributeFilter struct {
	logger *zap.Logger
}

var _ PolicyEvaluator = (*transparencyAttributeFilter)(nil)

func NewTransparencyAttributeFilter(settings component.TelemetrySettings) PolicyEvaluator {
	return &booleanAttributeFilter{
		logger: settings.Logger,
	}
}

// Evaluate looks at the trace data and returns a corresponding SamplingDecision.
func (taf *transparencyAttributeFilter) Evaluate(_ context.Context, _ pcommon.TraceID, trace *TraceData) (Decision, error) {
	trace.Lock()
	batches := trace.ReceivedBatches
	trace.Unlock()

	return hasSpanWithCondition(
		batches,
		func(span ptrace.Span) bool {
			insertTiltCheck(span)
			if v, ok := span.Attributes().Get(attrCheckFlag); ok {
				value := v.Bool()
				return value == false
			}
			return false
		}), nil
}
