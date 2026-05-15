package project

type Project struct {
	Name         string          `toml:"name"`
	Target       string          `toml:"target"`
	Layout       string          `toml:"layout"`
	RpasmVersion string          `toml:"rpasm_version"`
	Features     map[string]bool `toml:"features"`
	UserSource   UserSource      `toml:"user_source"`
	Bootloader   *Bootloader     `toml:"bootloader,omitempty"`

	// Studio-only fields (CLI ignores them). Persisted so the GUI can restore
	// which mode/example was active without losing user intent.
	StudioMode  string `toml:"studio_mode,omitempty"`  // "examples" | "custom"
	ExampleName string `toml:"example_name,omitempty"` // name of selected example (Examples mode)

	Path string `toml:"-"`
}

type UserSource struct {
	Files []string `toml:"files"`
}

// Bootloader, when non-nil, makes Build produce a complete chain UF2
// (firmware_<name>.uf2) containing SSBL + TSBL + app + footers. The app is
// linked at the slot-A base (0x10008000) instead of the bare-metal flash
// origin. TSBL flavor selects what kind of boot policy the chain ships with.
type Bootloader struct {
	TSBL string `toml:"tsbl"` // "bypass" | "ab"
}