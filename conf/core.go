package conf

import (
	"encoding/json"
	"fmt"
)

type CoreConfig struct {
	Type            string           `json:"Type"`
	Name            string           `json:"Name"`
	XrayConfig      *XrayConfig      `json:"-"`
	SingConfig      *SingConfig      `json:"-"`
	Hysteria2Config *Hysteria2Config `json:"-"`
}

type _CoreConfig CoreConfig

func (c *CoreConfig) UnmarshalJSON(b []byte) error {
	err := json.Unmarshal(b, (*_CoreConfig)(c))
	if err != nil {
		return fmt.Errorf("failed to unmarshal core config: %w", err)
	}

	// Pre-initialize configs to avoid nil pointer dereference
	c.XrayConfig = NewXrayConfig()
	c.SingConfig = NewSingConfig()
	c.Hysteria2Config = NewHysteria2Config()

	switch c.Type {
	case "xray":
		return json.Unmarshal(b, c.XrayConfig)
	case "sing":
		return json.Unmarshal(b, c.SingConfig)
	case "hysteria2":
		return json.Unmarshal(b, c.Hysteria2Config)
	default:
		// For unknown types, we don't error out but leave configs as default
		return nil
	}
}
