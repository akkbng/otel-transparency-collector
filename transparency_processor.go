package transparencyprocessor

import (
	"context"
	"github.com/akkbng/otel-transparency-collector/internal/filter/expr"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlspan"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"net/http"
	"sync"
	"time"

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
	server()
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
					continue
				}
				insertTiltCheck(span, tiltComponent)
			}
		}
	}
	return td, nil
}

func insertTiltCheck(span ptrace.Span, tiltComponent pcommon.Value) {
	//if tiltComponent value is not empty, add "true" as the value of the checkFlag attribute
	if tiltComponent.AsString() != "" {
		span.Attributes().PutBool(attrCheckFlag, true)
	} else {
		span.Attributes().PutBool(attrCheckFlag, false)
	}
}

func server() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/graph/data", getGraph)
	mux.HandleFunc("/api/graph/fields", getFields)

	http.ListenAndServe(":4318", mux)
}

// returns graph fields as json object, and in application/json format
func getFields(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Write([]byte(`{
  "edges_fields": [
    {
      "field_name": "id",
      "type": "string"
    },
    {
      "field_name": "source",
      "type": "string"
    },
    {
      "field_name": "target",
      "type": "string"
    },
    {
      "field_name": "mainStat",
      "type": "number"
    }
  ],
  "nodes_fields": [
    {
      "field_name": "id",
      "type": "string"
    },
    {
      "field_name": "title",
      "type": "string"
    },
    {
      "field_name": "mainStat",
      "type": "string"
    },
    {
      "field_name": "secondaryStat",
      "type": "number"
    },
    {
      "color": "red",
      "field_name": "arc__failed",
      "type": "number"
    },
    {
      "color": "green",
      "field_name": "arc__passed",
      "type": "number"
    },
    {
      "displayName": "Role",
      "field_name": "detail__role",
      "type": "string"
    }
  ]
}`))
}

// returns graph data as json object, and in application/json format
func getGraph(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Write([]byte(`{
    "edges": [
        {
            "id": "1",
            "mainStat": "53/s",
            "source": "1",
            "target": "2"
        }
    ],
    "nodes": [
        {
            "arc__failed": 0.7,
            "arc__passed": 0.3,
            "detail__zone": "load",
            "id": "1",
            "subTitle": "instance:#2",
            "title": "Service1"
        },
        {
            "arc__failed": 0.5,
            "arc__passed": 0.5,
            "detail__zone": "transform",
            "id": "2",
            "subTitle": "instance:#3",
            "title": "Service2"
        }
    ]
}`))
}
