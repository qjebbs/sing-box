package option

import (
	"reflect"

	"github.com/sagernet/sing-box/schema"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/json/badoption"
)

// _HealthCheckOptions is the settings for health check
type _HealthCheckOptions struct {
	Tag         string             `json:"tag,omitempty"`
	Interval    badoption.Duration `json:"interval"`
	Sampling    uint               `json:"sampling"`
	Destination string             `json:"destination"`
	DetourOf    []string           `json:"detour_of,omitempty"`
}

type (
	// HealthCheck is the top-level health check configuration.
	HealthCheck _HealthCheckOptions
	// HealthCheckOptions is a reference to a named health check declared in
	// `health_checks` (when Tag is set) or an inline health check options.
	HealthCheckOptions _HealthCheckOptions
)

// Options returns the HealthCheckOptions of this health check.
func (h HealthCheck) Options() HealthCheckOptions {
	options := HealthCheckOptions(h)
	options.Tag = ""
	return options
}

func (o HealthCheckOptions) IsEmpty() bool {
	if o.Tag != "" {
		return false
	}
	return reflect.ValueOf(_HealthCheckOptions(o)).IsZero()
}

func (h HealthCheck) MarshalJSON() ([]byte, error) {
	return json.Marshal(_HealthCheckOptions(h))
}

func (h *HealthCheck) UnmarshalJSON(content []byte) error {
	return json.Unmarshal(content, (*_HealthCheckOptions)(h))
}

func (o HealthCheckOptions) MarshalJSON() ([]byte, error) {
	if o.Tag != "" {
		return json.Marshal(o.Tag)
	}
	return json.Marshal(_HealthCheckOptions(o))
}

func (o *HealthCheckOptions) UnmarshalJSON(content []byte) error {
	if len(content) > 0 && content[0] == '"' {
		*o = HealthCheckOptions{}
		return json.Unmarshal(content, &o.Tag)
	}
	var options _HealthCheckOptions
	err := json.Unmarshal(content, &options)
	if err != nil {
		return err
	}
	options.Tag = ""
	*o = HealthCheckOptions(options)
	return nil
}

func describeHealthCheckObject(builder schema.Builder) (*schema.Node, error) {
	node := schema.StrictObject()
	err := builder.FlattenStruct(node, reflect.TypeFor[HealthCheck]())
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (h HealthCheck) DescribeSchema(builder schema.Builder) (*schema.Node, error) {
	return builder.Define("HealthCheck", func() (*schema.Node, error) {
		return describeHealthCheckObject(builder)
	})
}

func (o HealthCheckOptions) DescribeSchema(builder schema.Builder) (*schema.Node, error) {
	return builder.Define("HealthCheckReference", func() (*schema.Node, error) {
		object, err := describeHealthCheckObject(builder)
		if err != nil {
			return nil, err
		}
		object.Properties.Remove("tag")
		return schema.AnyOf(schema.TagReferenceNode("health_check"), object), nil
	})
}
