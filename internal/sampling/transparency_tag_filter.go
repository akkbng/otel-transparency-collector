package sampling

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/akkbng/otel-transparency-collector/internal/filter/expr"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlspan"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"net/http"
	"strings"
)

var spec *tiltSpec //create a new tiltSpec struct

const (
	attrCheckFlag           = "tilt.check_flag"
	attrCategories          = "tilt.dataDisclosed.category"
	attrLegalBases          = "tilt.legal_bases"
	attrLegitimateInterests = "tilt.legitimate_interests"
	attrStorages            = "tilt.storage_durations"
	attrPurposes            = "tilt.purposes"
	attrAutomatedDecision   = "tilt.automated_decision_making"

	attrServiceName = "service.name" //resource attribute, not trace span attribute
)

type tiltSpec struct {
	DataDisclosed []struct {
		ServiceId string `json:"_id"`
		Category  string `json:"category"`
		Purposes  []struct {
			Purpose     string `json:"purpose"`
			Description string `json:"description"`
		} `json:"purposes"`
		LegalBases []struct {
			Reference   string `json:"reference"`
			Description string `json:"description"`
		} `json:"legalBases"`
		LegitimateInterests []struct {
			Exists    bool   `json:"exists"`
			Reasoning string `json:"reasoning"`
		} `json:"legitimateInterests"`
		Recipients []struct {
			Category string `json:"category"`
		} `json:"recipients"`
		Storage []struct {
			Temporal              []any    `json:"temporal"`
			PurposeConditional    []string `json:"purposeConditional"`
			LegalBasisConditional []any    `json:"legalBasisConditional"`
			AggregationFunction   string   `json:"aggregationFunction"`
		} `json:"storage"`
		NonDisclosure struct {
			LegalRequirement      bool   `json:"legalRequirement"`
			ContractualRegulation bool   `json:"contractualRegulation"`
			ObligationToProvide   bool   `json:"obligationToProvide"`
			Consequences          string `json:"consequences"`
		} `json:"nonDisclosure"`
	} `json:"dataDisclosed"`
	AutomatedDecisionMaking struct {
		InUse bool `json:"inUse"`
	} `json:"automatedDecisionMaking"`
}

type transparencyAttributeFilter struct {
	logger   *zap.Logger
	skipExpr expr.BoolExpr[ottlspan.TransformContext]
	tiltUrl  string
}

var _ PolicyEvaluator = (*transparencyAttributeFilter)(nil)

func NewTransparencyAttributeFilter(settings component.TelemetrySettings, tiltUrl string) PolicyEvaluator {
	retrieveTiltFile(tiltUrl)

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
				currentServiceName, ok := resource.Attributes().Get(attrServiceName)
				if !ok {
					continue
				}

				if tiltCheckSampling(currentServiceName, span) == true {
					return Sampled, nil
				}
			}
		}
	}
	return NotSampled, nil
}

// Current implementation does not sample if the first span comes from a service that is not in the tilt file
func tiltCheckSampling(currentServiceName pcommon.Value, span ptrace.Span) bool {
	for _, service := range spec.DataDisclosed {
		if currentServiceName.AsString() == service.ServiceId {
			//check if the attrCategories attribute of the span is listed in the services' categories. Category is a string with multiple values separated by semi-colons
			tiltCategoryAttribute, ok := span.Attributes().Get(attrCategories)
			if !ok {
				span.Attributes().PutBool(attrCheckFlag, false)
				return true //if span name is in the service list, but attrCategories does not exist, check flag is false - sample
			}
			if tiltCategoryAttribute.AsString() != "" && strings.Contains(service.Category, tiltCategoryAttribute.AsString()) {
				span.Attributes().PutBool(attrCheckFlag, true)
				return false //if span name is in the service list and attrCategories is found, check flag is true and return true - don't sample
			} else {
				span.Attributes().PutBool(attrCheckFlag, false)
				return true //if span name is in the service list, but attrCategories is not matching, check flag is false - sample
			}
		} else {
			continue //continue to next service
		}
	}
	return false //if the current service name is not one of the ServiceIds in the spec struct, it doesn't contain personal data - don't sample
}

func retrieveTiltFile(url string) {
	// Send an HTTP GET request to the URL
	response, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error fetching tilt file: %v", err)
	}

	defer response.Body.Close()
	d := json.NewDecoder(response.Body)
	if err := d.Decode(&spec); err != nil {
		fmt.Printf("Error decoding tilt file: %v", err)
	}
}
