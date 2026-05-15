package catalog

type Module struct {
	Symbol      string   `toml:"symbol"`
	Name        string   `toml:"name"`
	Category    string   `toml:"category"`
	Order       int      `toml:"order"`
	Default     bool     `toml:"default"`
	Description string   `toml:"description"`
	Sources     []string `toml:"sources"`
	Includes    []string `toml:"includes"`
	Requires    []string `toml:"requires"`

	Path string `toml:"-"`
}

type Target struct {
	Name            string   `toml:"name"`
	Arch            string   `toml:"arch"`
	ToolchainPrefix string   `toml:"toolchain_prefix"`
	AsFlags         []string `toml:"as_flags"`
	AsIncludes      []string `toml:"as_includes"`
	LdFlags         []string `toml:"ld_flags"`
	LdScriptFlash   string   `toml:"ld_script_flash"`
	LdScriptSram    string   `toml:"ld_script_sram"`
	FlashLoadAddr   uint32   `toml:"flash_load_addr"`
	SramLoadAddr    uint32   `toml:"sram_load_addr"`
	Uf2FamilyID     uint32   `toml:"uf2_family_id"`
}

type Catalog struct {
	Modules map[string]*Module
	Targets map[string]*Target
}