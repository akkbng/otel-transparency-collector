package transparencyprocessor

import (
	"github.com/akkbng/otel-transparency-collector/internal/filter/filterconfig"
	"go.opentelemetry.io/collector/component"
)

type Config struct {
	filterconfig.MatchConfig `mapstructure:",squash"`
}

var _ component.Config = (*Config)(nil)
