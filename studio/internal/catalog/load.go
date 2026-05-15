package catalog

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

func Load(root string) (*Catalog, error) {
	cat := &Catalog{
		Modules: make(map[string]*Module),
		Targets: make(map[string]*Target),
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".toml") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, "targets"+string(filepath.Separator)) {
			t := &Target{}
			if _, err := toml.DecodeFile(path, t); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if t.Name == "" {
				return fmt.Errorf("%s: target missing name", path)
			}
			cat.Targets[t.Name] = t
			return nil
		}
		m := &Module{}
		if _, err := toml.DecodeFile(path, m); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if m.Symbol == "" {
			return fmt.Errorf("%s: module missing symbol", path)
		}
		m.Path = path
		if _, dup := cat.Modules[m.Symbol]; dup {
			return fmt.Errorf("%s: duplicate symbol %q", path, m.Symbol)
		}
		cat.Modules[m.Symbol] = m
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return cat, nil
}
