package project

type Project struct {
	Name         string          `toml:"name"`
	Target       string          `toml:"target"`
	Layout       string          `toml:"layout"`
	RpasmVersion string          `toml:"rpasm_version"`
	Features     map[string]bool `toml:"features"`
	UserSource   UserSource      `toml:"user_source"`

	// Studio-only fields (CLI ignores them). Persisted so the GUI can restore
	// which mode/example was active without losing user intent.
	StudioMode  string `toml:"studio_mode,omitempty"`  // "examples" | "custom"
	ExampleName string `toml:"example_name,omitempty"` // name of selected example (Examples mode)

	Path string `toml:"-"`
}

type UserSource struct {
	Files []string `toml:"files"`
}