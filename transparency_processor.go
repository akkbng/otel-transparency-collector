package transparencyprocessor

import (
	"context"
	"github.com/akkbng/otel-transparency-collector/internal/filterspan"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"sync"
	"time"
)

var processorCapabilities = consumer.Capabilities{MutatesData: true}

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
}

type transparencyProcessor struct {
	logger    *zap.Logger
	exportCtx context.Context

	timeout time.Duration

	telemetryLevel configtelemetry.Level

	serviceMap map[string]string

	mu              sync.RWMutex
	attributesCache map[string]tiltAttributes
	include         filterspan.Matcher
	exclude         filterspan.Matcher
	//attrProc        *attraction.AttrProc
}

func newTransparencyProcessor(set component.ProcessorCreateSettings, include, exclude filterspan.Matcher, serviceMap map[string]string) *transparencyProcessor {
	tp := new(transparencyProcessor)
	tp.logger = set.Logger
	tp.attributesCache = make(map[string]tiltAttributes)
	tp.mu = sync.RWMutex{}
	tp.serviceMap = serviceMap
	tp.include = include
	tp.exclude = exclude

	return tp
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
			library := ils.Scope()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if filterspan.SkipSpan(a.include, a.exclude, span, resource, library) {
					continue
				}

				tiltComponent, ok := span.Attributes().Get(attrCategories)
				if !ok {
					continue
				}
				//if tiltComponent value is not empty, add "true" as the value of the checkFlag attribute
				if tiltComponent.AsString() != "" {
					span.Attributes().InsertBool(attrCheckFlag, true)
				} else {
					span.Attributes().InsertBool(attrCheckFlag, false)
				}

				//k := attributeKey(tHost.AsString(), span.Name())
				//a.mu.RLock()
				//attr, ok := a.attributesCache[k]
				//a.mu.RUnlock()
				//if !ok {
				//	a.logger.Info("no tiltAttributes found in cache for key", zap.String("key", k))
				//	attributes, err := a.updateAttributes(tHost.AsString(), span.Name())
				//	if err != nil {
				//		a.logger.Warn(fmt.Sprintf("error updating tiltAttributes: %v", err))
				//	}
				//	attr = attributes
				//}
				//
				//insertAttributes(span, attrCategories, attr.categories)
				//insertAttributes(span, attrLegalBases, attr.legalBases)
				//insertAttributes(span, attrStorages, attr.storages)
				//insertAttributes(span, attrPurposes, attr.purposes)
				//if attr.automatedDecision {
				//	span.Attributes().InsertBool(attrAutomatedDecision, attr.automatedDecision)
				//}
				//span.Attributes().InsertString(attrLegitimateInterests, fmt.Sprintf("%v", attr.legitimateInterests))
			}
		}
	}
	return td, nil
}

//func insertAttributes(span ptrace.Span, key string, values []string) {
//	if len(values) == 0 {
//		return
//	}
//	b := pcommon.NewSlice()
//	b.EnsureCapacity(len(values))
//	for _, c := range values {
//		v := b.AppendEmpty()
//		v.SetStringVal(c)
//	}
//	vs := pcommon.NewValueSlice()
//	b.CopyTo(vs.SliceVal())
//	span.Attributes().Insert(key, vs)
//}
