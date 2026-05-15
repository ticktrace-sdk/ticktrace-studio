package project

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

func Load(path string) (*Project, error) {
	p := &Project{}
	if _, err := toml.DecodeFile(path, p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("%s: project missing name", path)
	}
	if p.Target == "" {
		return nil, fmt.Errorf("%s: project missing target", path)
	}
	if p.Layout == "" {
		p.Layout = "flash"
	}
	if p.Layout != "flash" && p.Layout != "sram" {
		return nil, fmt.Errorf("%s: layout must be \"flash\" or \"sram\", got %q", path, p.Layout)
	}
	if p.Features == nil {
		p.Features = make(map[string]bool)
	}
	p.Path = path
	return p, nil
}
