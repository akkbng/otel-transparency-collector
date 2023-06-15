// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filterottl // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/filter/filterottl"

import (
	"fmt"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.uber.org/zap"

	"github.com/akkbng/otel-transparency-collector/internal/filter/expr"
	"github.com/akkbng/otel-transparency-collector/internal/filter/filterconfig"
	"github.com/akkbng/otel-transparency-collector/internal/filter/filterset"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlspan"
)

const (
	serviceNameStaticStatement        = `resource.attributes["service.name"] == "%v"`
	spanNameStaticStatement           = `name == "%v"`
	spanKindStaticStatement           = `kind.deprecated_string == "%v"`
	scopeNameStaticStatement          = `instrumentation_scope.name == "%v"`
	scopeVersionStaticStatement       = `instrumentation_scope.version == "%v"`
	attributesStaticStatement         = `attributes["%v"] == %v`
	resourceAttributesStaticStatement = `resource.attributes["%v"] == %v`

	serviceNameRegexStatement        = `IsMatch(resource.attributes["service.name"], "%v")`
	spanNameRegexStatement           = `IsMatch(name, "%v")`
	spanKindRegexStatement           = `IsMatch(kind.deprecated_string, "%v")`
	scopeNameRegexStatement          = `IsMatch(instrumentation_scope.name, "%v")`
	scopeVersionRegexStatement       = `IsMatch(instrumentation_scope.version, "%v")`
	attributesRegexStatement         = `IsMatch(attributes["%v"], "%v")`
	resourceAttributesRegexStatement = `IsMatch(resource.attributes["%v"], "%v")`
)

func NewSpanSkipExprBridge(mc *filterconfig.MatchConfig) (expr.BoolExpr[ottlspan.TransformContext], error) {
	statements := make([]string, 0, 2)
	if mc.Include != nil {
		statement, err := createStatement(*mc.Include)
		if err != nil {
			return nil, err
		}
		statements = append(statements, fmt.Sprintf("not (%v)", statement))
	}

	if mc.Exclude != nil {
		statement, err := createStatement(*mc.Exclude)
		if err != nil {
			return nil, err
		}
		statements = append(statements, fmt.Sprintf("%v", statement))
	}

	return NewBoolExprForSpan(statements, StandardSpanFuncs(), ottl.PropagateError, component.TelemetrySettings{Logger: zap.NewNop()})
}

func createStatement(mp filterconfig.MatchProperties) (string, error) {
	serviceNameConditions, spanNameConditions, spanKindConditions, scopeNameConditions, scopeVersionConditions, attributeConditions, resourceAttributeConditions, err := createConditions(mp)
	if err != nil {
		return "", err
	}
	var conditions []string
	var format string
	if serviceNameConditions != nil {
		if len(serviceNameConditions) > 1 {
			format = "(%v)"
		} else {
			format = "%v"
		}
		conditions = append(conditions, fmt.Sprintf(format, strings.Join(serviceNameConditions, " or ")))
	}
	if spanNameConditions != nil {
		if len(spanNameConditions) > 1 {
			format = "(%v)"
		} else {
			format = "%v"
		}
		conditions = append(conditions, fmt.Sprintf(format, strings.Join(spanNameConditions, " or ")))
	}
	if spanKindConditions != nil {
		if len(spanKindConditions) > 1 {
			format = "(%v)"
		} else {
			format = "%v"
		}
		conditions = append(conditions, fmt.Sprintf(format, strings.Join(spanKindConditions, " or ")))
	}
	if scopeNameConditions != nil {
		if len(scopeNameConditions) > 1 {
			format = "(%v)"
		} else {
			format = "%v"
		}
		conditions = append(conditions, fmt.Sprintf(format, strings.Join(scopeNameConditions, " or ")))
	}
	if scopeVersionConditions != nil {
		if len(scopeVersionConditions) > 1 {
			format = "(%v)"
		} else {
			format = "%v"
		}
		conditions = append(conditions, fmt.Sprintf(format, strings.Join(scopeVersionConditions, " or ")))
	}
	if attributeConditions != nil {
		conditions = append(conditions, fmt.Sprintf("%v", strings.Join(attributeConditions, " and ")))
	}
	if resourceAttributeConditions != nil {
		conditions = append(conditions, fmt.Sprintf("%v", strings.Join(resourceAttributeConditions, " and ")))
	}
	return strings.Join(conditions, " and "), nil
}

func createConditions(mp filterconfig.MatchProperties) ([]string, []string, []string, []string, []string, []string, []string, error) {
	serviceNameStatement, spanNameStatement, spanKindStatement, scopeNameStatement, scopeVersionStatement, attrStatement, resourceAttrStatement, err := createStatementTemplates(mp.MatchType)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, err
	}

	serviceNameConditions := createBasicConditions(serviceNameStatement, mp.Services)
	spanNameConditions := createBasicConditions(spanNameStatement, mp.SpanNames)
	spanKindConditions := createBasicConditions(spanKindStatement, mp.SpanKinds)
	scopeNameConditions, scopeVersionConditions := createLibraryConditions(scopeNameStatement, scopeVersionStatement, mp.Libraries)
	attributeConditions := createAttributeConditions(attrStatement, mp.Attributes, mp.MatchType)
	resourceAttributeConditions := createAttributeConditions(resourceAttrStatement, mp.Resources, mp.MatchType)

	return serviceNameConditions, spanNameConditions, spanKindConditions, scopeNameConditions, scopeVersionConditions, attributeConditions, resourceAttributeConditions, nil
}

func createStatementTemplates(matchType filterset.MatchType) (string, string, string, string, string, string, string, error) {
	switch matchType {
	case filterset.Strict:
		return serviceNameStaticStatement, spanNameStaticStatement, spanKindStaticStatement, scopeNameStaticStatement, scopeVersionStaticStatement, attributesStaticStatement, resourceAttributesStaticStatement, nil
	case filterset.Regexp:
		return serviceNameRegexStatement, spanNameRegexStatement, spanKindRegexStatement, scopeNameRegexStatement, scopeVersionRegexStatement, attributesRegexStatement, resourceAttributesRegexStatement, nil
	default:
		return "", "", "", "", "", "", "", filterset.NewUnrecognizedMatchTypeError(matchType)
	}
}

func createBasicConditions(template string, input []string) []string {
	var conditions []string
	for _, serviceName := range input {
		conditions = append(conditions, fmt.Sprintf(template, serviceName))
	}
	return conditions
}

func createLibraryConditions(nameTemplate string, versionTemplate string, libraries []filterconfig.InstrumentationLibrary) ([]string, []string) {
	var scopeNameConditions []string
	var scopeVersionConditions []string
	for _, scope := range libraries {
		scopeNameConditions = append(scopeNameConditions, fmt.Sprintf(nameTemplate, scope.Name))
		if scope.Version != nil {
			scopeVersionConditions = append(scopeVersionConditions, fmt.Sprintf(versionTemplate, *scope.Version))
		}
	}
	return scopeNameConditions, scopeVersionConditions
}

func createAttributeConditions(template string, input []filterconfig.Attribute, matchType filterset.MatchType) []string {
	var attributeConditions []string
	for _, attribute := range input {
		var value any
		if matchType == filterset.Strict {
			value = convertAttribute(attribute.Value)
		} else {
			value = attribute.Value
		}
		attributeConditions = append(attributeConditions, fmt.Sprintf(template, attribute.Key, value))
	}
	return attributeConditions
}

func convertAttribute(value any) string {
	switch val := value.(type) {
	case string:
		return fmt.Sprintf(`"%v"`, val)
	default:
		return fmt.Sprintf(`%v`, val)
	}
}
