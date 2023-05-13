package transparencyprocessor

import (
	"github.com/akkbng/otel-transparency-collector/internal/filterconfig"
	"go.opentelemetry.io/collector/config"
)

type Config struct {
	config.ProcessorSettings `mapstructure:",squash"` // squash ensures fields are correctly decoded in embedded struct
	filterconfig.MatchConfig `mapstructure:",squash"`
	ServiceMap               map[string]string `mapstructure:"serviceMap"`
}

var _ config.Processor = (*Config)(nil)
