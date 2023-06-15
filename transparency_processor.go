package transparencyprocessor

import (
	"context"
	"github.com/akkbng/otel-transparency-collector/internal/filter/expr"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlspan"
	"go.opentelemetry.io/collector/consumer"
	"sync"
	"time"

	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

var processorCapabilities = consumer.Capabilities{MutatesData: true}

// this is gonna be deleted after we get the services list from tilt file
var serviceList = [5]string{"cartservice", "emailservice", "quoteservice", "frontend", "paymentservice"}

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

type transparencyProcessor struct {
	logger    *zap.Logger
	exportCtx context.Context

	timeout time.Duration

	mu              sync.RWMutex
	attributesCache map[string]tiltAttributes
	skipExpr        expr.BoolExpr[ottlspan.TransformContext]
}

func newTransparencyProcessor(logger *zap.Logger, skipExpr expr.BoolExpr[ottlspan.TransformContext]) *transparencyProcessor {
	return &transparencyProcessor{
		logger:   logger,
		skipExpr: skipExpr,
	}
}

func (a *transparencyProcessor) processTraces(ctx context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		resource := rs.Resource()
		ilss := rs.ScopeSpans()
		for j := 0; j < ilss.Len(); j++ {
			ils := ilss.At(j)
			spans := ils.Spans()
			scope := ils.Scope()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if a.skipExpr != nil {
					skip, err := a.skipExpr.Eval(ctx, ottlspan.NewTransformContext(span, scope, resource))
					if err != nil {
						return td, err
					}
					if skip {
						continue
					}
				}
				tiltComponent, ok := span.Attributes().Get(attrCategories)
				if !ok {
					//if the span does not have the tiltComponent attribute, but the service name is in the serviceList, set the checkFlag attribute to false
					serviceName, ok := resource.Attributes().Get("service.name")
					if !ok {
						continue
					}
					for _, service := range serviceList {
						if serviceName.AsString() == service {
							span.Attributes().PutBool(attrCheckFlag, false)
						}
					}
					continue
				}

				//if tiltComponent value is not empty, add "true" as the value of the checkFlag attribute
				if tiltComponent.AsString() != "" {
					span.Attributes().PutBool(attrCheckFlag, true)
				} else {
					span.Attributes().PutBool(attrCheckFlag, false)
				}

			}
		}
	}
	return td, nil
}
