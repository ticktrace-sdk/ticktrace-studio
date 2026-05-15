package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	giowidget "gioui.org/widget"

	"github.com/amken3d/immygo/theme"
	"github.com/amken3d/immygo/ui"

	"github.com/amken3d/rp-asm/studio/internal/build"
	"github.com/amken3d/rp-asm/studio/internal/catalog"
	"github.com/amken3d/rp-asm/studio/internal/flash"
	"github.com/amken3d/rp-asm/studio/internal/project"
)

const udevRulePath = "/etc/udev/rules.d/99-rpasmboot.rules"

// Persistent scroll list for the build-output panel. ui.Scroll constructs a
// fresh widget per frame, which would reset scroll position; reusing this
// giowidget.List keeps it stable. (See ImmyGo ui-showcase for the pattern.)
var outputScroll = giowidget.List{List: layout.List{Axis: layout.Vertical}}

func main() {
	root := studioRoot()
	cat, err := catalog.Load(filepath.Join(root, "catalog"))
	if err != nil {
		fatal("load catalog: %v", err)
	}

	app := newApp(root, cat)
	app.run()
}

type appState struct {
	root    string
	catalog *catalog.Catalog

	// Modules sorted by Order for canonical display.
	modules []*catalog.Module
	// One checkbox per module symbol; created once and reused across frames.
	checks map[string]*ui.CheckboxView
	// Categories in canonical order, derived from module Order.
	categories []string

	// Target dropdown — populated from catalog.Targets.
	targetDropdown     *ui.DropdownView
	bootloaderDropdown *ui.DropdownView
	targetNames    []string

	// Example picker — dropdown is populated from <parent>/examples/*.S.
	// allExampleNames/allExamplePaths are the unfiltered universe (index 0 is
	// always the synthetic 'blinky' from src/main.S). exampleFilter is the
	// search string; dropdown contents are recomputed each frame from the
	// filter and re-pushed into the underlying widget via SetItems.
	exampleDropdown  *ui.DropdownView
	allExampleNames  []string
	allExamplePaths  []string
	exampleFilter    *ui.InputView
	lastFilterApplied string

	// Layout dropdown — flash or sram.
	layoutDropdown *ui.DropdownView

	// Project name input.
	nameInput *ui.InputView

	// Mode toggle: "examples" (run a canned .S from examples/) vs "custom"
	// (user-supplied source path + feature toggles for scaffolded projects).
	mode       *ui.State[string]
	modeTabBar *ui.TabBarView

	// Sticky per-mode layout preferences ("flash" | "sram"). Captured when
	// switching away from a mode, restored when switching back, so a manual
	// override survives tab toggles. Initial values match the per-mode default
	// (SRAM for Examples, flash for Custom).
	examplesLayout string
	customLayout   string

	// Custom-mode only — path to a user-provided .S file.
	userSourceInput *ui.InputView

	// Project save/load — file path defaults to <root>/projects/<name>.rpasm.toml
	// at save time but the user can override.
	projectPathInput *ui.InputView

	// Build state. The "currently flashable UF2" is NOT cached here — it's
	// derived from inputs every frame via expectedUf2(), so changing the
	// example dropdown immediately retargets Flash without needing a rebuild.
	status    *ui.State[string]
	logLines  *ui.State[[]string]
	problems  *ui.State[[]Problem]
	memory     *ui.State[*build.MemoryUsage]
	bootloader *ui.State[*build.BootloaderUsage]
	activeTab  *ui.State[string] // "output" | "problems" | "memory"
	building *ui.State[bool]
	flashing *ui.State[bool]
	udevOK     *ui.State[bool]
	boardSt    *ui.State[flash.BoardState]
	slotInfo   *ui.State[[]flash.SlotInfo]
	slotErr    *ui.State[string]
	querying   *ui.State[bool]

	logMu sync.Mutex
}

// Problem is one parsed entry from `as` or `ld` stderr. v1 only recognises the
// GNU `<file>:<line>: <severity>: <msg>` form. ld "undefined reference" lines
// (which lack a line number) are deferred.
type Problem struct {
	File     string
	Line     int
	Severity string // "error" | "warning" | "fatal" | "note"
	Message  string
}

func newApp(root string, cat *catalog.Catalog) *appState {
	modOrder := make([]*catalog.Module, 0, len(cat.Modules))
	for _, m := range cat.Modules {
		modOrder = append(modOrder, m)
	}
	sort.Slice(modOrder, func(i, j int) bool {
		if modOrder[i].Order != modOrder[j].Order {
			return modOrder[i].Order < modOrder[j].Order
		}
		return modOrder[i].Symbol < modOrder[j].Symbol
	})

	seenCat := map[string]bool{}
	var cats []string
	for _, m := range modOrder {
		if !seenCat[m.Category] {
			seenCat[m.Category] = true
			cats = append(cats, m.Category)
		}
	}

	checks := make(map[string]*ui.CheckboxView, len(modOrder))
	for _, m := range modOrder {
		checks[m.Symbol] = ui.Checkbox(m.Name, m.Default)
	}

	tgtNames := make([]string, 0, len(cat.Targets))
	for n := range cat.Targets {
		tgtNames = append(tgtNames, n)
	}
	sort.Strings(tgtNames)
	tgtDropdown := ui.Dropdown(tgtNames...).Placeholder("Target")
	if len(tgtNames) > 0 {
		tgtDropdown.SetSelected(0)
	}

	// Default Layout depends on the initial mode (Examples). Examples mode
	// prefers SRAM (faster iteration, doesn't wear flash); Custom mode prefers
	// flash (conservative — persists across power cycles). Switching tabs
	// flips the default; user can still override per-build.
	layoutDropdown := ui.Dropdown("flash", "sram").SetSelected(1) // 1 = sram

	// Bootloader knob. Index 0 = none (default; produces <name>.uf2),
	// 1 = bypass (single-slot chain), 2 = ab (A/B + rollback). The build
	// engine requires layout=flash when this is non-none — set both
	// together or you'll get a build-time error.
	bootloaderDropdown := ui.Dropdown("(no bootloader)", "bypass", "ab").SetSelected(0)

	nameInput := ui.Input().Placeholder("Project name")
	nameInput.SetValue("blinky")

	exampleNames, examplePaths := loadExamples(root)
	var exampleDropdown *ui.DropdownView
	var exampleFilter *ui.InputView
	if len(exampleNames) > 0 {
		// Index 0 is the canonical hardware blinky from loadExamples; it's
		// always the default. blinky_v01 is intentionally further down.
		exampleDropdown = ui.Dropdown(exampleNames...).Placeholder("Example").SetSelected(0)
		exampleFilter = ui.Input().Placeholder("filter examples...")
	}

	userSourceInput := ui.Input().Placeholder("path to .S file (e.g. ../src/myapp.S)")
	projectPathInput := ui.Input().Placeholder("project.rpasm.toml path")

	a := &appState{
		root:           root,
		catalog:        cat,
		modules:        modOrder,
		checks:         checks,
		categories:     cats,
		targetDropdown:     tgtDropdown,
		bootloaderDropdown: bootloaderDropdown,
		targetNames:      tgtNames,
		exampleDropdown:  exampleDropdown,
		allExampleNames:  exampleNames,
		allExamplePaths:  examplePaths,
		exampleFilter:    exampleFilter,
		layoutDropdown:   layoutDropdown,
		nameInput:        nameInput,
		userSourceInput:  userSourceInput,
		projectPathInput: projectPathInput,
		mode:             ui.NewState("examples"),
		examplesLayout:   "sram",
		customLayout:     "flash",
		status:     ui.NewState("Ready."),
		logLines:   ui.NewState([]string{}),
		problems:   ui.NewState([]Problem{}),
		memory:     ui.NewState[*build.MemoryUsage](nil),
		bootloader: ui.NewState[*build.BootloaderUsage](nil),
		activeTab:  ui.NewState("output"),
		building: ui.NewState(false),
		flashing: ui.NewState(false),
		udevOK:   ui.NewState(detectUdevRule()),
		boardSt:  ui.NewState(flash.DetectBoard()),
		slotInfo: ui.NewState[[]flash.SlotInfo](nil),
		slotErr:  ui.NewState(""),
		querying: ui.NewState(false),
	}
	a.modeTabBar = ui.TabBar("Examples", "Custom Project").
		SetSelected(0).
		OnSelect(func(idx int) {
			// Capture the user's current layout choice for the mode we're
			// leaving, then restore the stored choice for the mode we're
			// entering. SetSelected on the dropdown is programmatic and does
			// NOT fire its own OnSelect, so this stays single-source-of-truth.
			cur := a.layoutDropdown.SelectedText()
			switch a.mode.Get() {
			case "examples":
				if cur != "" {
					a.examplesLayout = cur
				}
			case "custom":
				if cur != "" {
					a.customLayout = cur
				}
			}
			// Switching modes resets the bootloader dropdown to "(no bootloader)"
			// since Examples mode never wants a chain build.
			if a.bootloaderDropdown != nil {
				a.bootloaderDropdown.SetSelected(0)
			}
			var next string
			if idx == 1 {
				a.mode.Set("custom")
				next = a.customLayout
			} else {
				a.mode.Set("examples")
				next = a.examplesLayout
			}
			if next == "sram" {
				a.layoutDropdown.SetSelected(1)
			} else {
				a.layoutDropdown.SetSelected(0)
			}
		})
	return a
}

// expectedUf2 returns the canonical UF2 output path for the current
// (Name, Example) inputs — i.e. the file `Build` would produce, and the file
// `Flash` will send to the board. Computed fresh every frame so dropdown
// changes are reflected immediately.
func (a *appState) expectedUf2() string {
	name := a.derivedName()
	if name == "" {
		return ""
	}
	// Bootloader builds produce firmware_<name>.uf2; bare flash/sram builds
	// produce <name>.uf2. Match build.Build's output naming exactly so the
	// Flash button enables and the Flash action targets the right file.
	fname := name + ".uf2"
	if a.currentBootloader() != nil {
		fname = "firmware_" + name + ".uf2"
	}
	return filepath.Join(filepath.Dir(a.root), "build", name, fname)
}

// derivedName mirrors runBuild's name logic exactly. Keep them in sync.
func (a *appState) derivedName() string {
	name := a.nameInput.Value()
	exName, _ := a.currentExample()
	if exName != "" && (name == "" || name == "blinky") {
		name = exName
	}
	return name
}

func (a *appState) haveBuiltUf2() bool {
	p := a.expectedUf2()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func (a *appState) run() {
	// Background BOOTSEL poller: refresh every 2 s so the Tools row reflects
	// the current /proc/mounts state without user action. Gio invalidates the
	// frame whenever any ui.State changes, so the badge updates automatically.
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			now := flash.DetectBoard()
			prev := a.boardSt.Get()
			if now != prev {
				a.boardSt.Set(now)
			}
		}
	}()

	ui.Run("rp-asm Studio", func() ui.View {
		return ui.VStack(
			a.modeTabBar,
			ui.Divider(),
			a.topBar(),
			a.toolsRow(),
			a.projectRow(),
			ui.Expanded(
				ui.HStack(
					ui.Flex(1, a.leftPane()),
					ui.Flex(2, a.outputPane()),
				).Spacing(ui.SpaceMd),
			),
		).Spacing(ui.SpaceSm).Padding(ui.SpaceMd)
	}, ui.Size(1100, 720))
}

// projectRow shows the Save/Load controls for round-tripping the current GUI
// state to a .rpasm.toml file.
func (a *appState) projectRow() ui.View {
	return ui.HStack(
		ui.Text("Project:").Small(),
		a.projectPathInput,
		ui.Button("Browse...").OnClick(a.onBrowseProject),
		ui.Button("Save").OnClick(a.onSaveProject),
		ui.Button("Load").OnClick(a.onLoadProject),
	).Spacing(ui.SpaceMd).Center()
}

func (a *appState) onSaveProject() {
	path := strings.TrimSpace(a.projectPathInput.Value())
	if path == "" {
		// Derive a default: <root>/projects/<derivedName>.rpasm.toml
		name := a.derivedName()
		if name == "" {
			name = "project"
		}
		path = filepath.Join(a.root, "projects", name+".rpasm.toml")
		a.projectPathInput.SetValue(path)
	}
	proj := a.captureProject()
	if err := project.Save(proj, path); err != nil {
		a.status.Set("Save failed.")
		a.log("ERROR: " + err.Error())
		return
	}
	a.status.Set("Saved " + path)
	a.log("saved " + path)
}

func (a *appState) onLoadProject() {
	raw := strings.TrimSpace(a.projectPathInput.Value())
	if raw == "" {
		a.status.Set("Load failed: path required.")
		a.log("ERROR: enter a project file path or click Browse... before clicking Load.")
		return
	}
	path, err := a.resolveProjectPath(raw)
	if err != nil {
		a.status.Set("Load failed.")
		a.log("ERROR: " + err.Error())
		return
	}
	proj, err := project.Load(path)
	if err != nil {
		a.status.Set("Load failed.")
		a.log("ERROR: " + err.Error())
		return
	}
	// Echo the resolved path back into the input so subsequent saves go to
	// the same location and the user sees what got opened.
	a.projectPathInput.SetValue(path)
	a.applyProject(proj)
	a.status.Set("Loaded " + path)
	a.log("loaded " + path)
}

// resolveProjectPath turns a user-typed path (possibly relative) into an
// absolute one. Tries, in order: as-is (absolute or CWD-relative), studio
// root, SDK root (parent of studio). Returns a wrapped error listing the
// candidates if none exist — much friendlier than "no such file or folder".
func (a *appState) resolveProjectPath(raw string) (string, error) {
	if filepath.IsAbs(raw) {
		if _, err := os.Stat(raw); err == nil {
			return raw, nil
		}
		return "", fmt.Errorf("%s: not found", raw)
	}
	parent := filepath.Dir(a.root)
	candidates := []string{
		raw,                              // CWD-relative
		filepath.Join(a.root, raw),       // studio-root-relative
		filepath.Join(parent, raw),       // SDK-root-relative
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, err := filepath.Abs(c)
			if err != nil {
				return c, nil
			}
			return abs, nil
		}
	}
	return "", fmt.Errorf("project file not found. Tried:\n  %s", strings.Join(candidates, "\n  "))
}

func (a *appState) onBrowseProject() {
	start := filepath.Join(a.root, "testdata")
	if _, err := os.Stat(start); err != nil {
		start = a.root
	}
	picked, err := pickFile("Choose project TOML", start)
	if err != nil {
		a.status.Set("Browse failed.")
		a.log("ERROR: " + err.Error())
		return
	}
	if picked == "" {
		return // user cancelled — no fuss
	}
	a.projectPathInput.SetValue(picked)
	// Auto-load: Browse-then-click-Build is the obvious flow; making the
	// user remember to click Load between them is a footgun.
	a.onLoadProject()
}

// captureProject snapshots the current GUI state into a Project for saving.
func (a *appState) captureProject() *project.Project {
	mode := a.mode.Get()
	p := &project.Project{
		Name:         strings.TrimSpace(a.nameInput.Value()),
		Target:       a.currentTarget(),
		Layout:       a.currentLayout(),
		RpasmVersion: "1.0",
		StudioMode:   mode,
		Features:     map[string]bool{},
		Bootloader:   a.currentBootloader(),
	}
	if p.Name == "" {
		if mode == "examples" {
			if n, _ := a.currentExample(); n != "" {
				p.Name = n
			}
		}
		if p.Name == "" {
			p.Name = "project"
		}
	}
	switch mode {
	case "examples":
		exName, exPath := a.currentExample()
		p.ExampleName = exName
		if exPath != "" {
			// Persist a portable relative path (relative to studio root's
			// parent — i.e. the SDK root, which is the canonical CWD).
			parent := filepath.Dir(a.root)
			if rel, err := filepath.Rel(parent, exPath); err == nil && !strings.HasPrefix(rel, "..") {
				p.UserSource.Files = []string{rel}
			} else {
				p.UserSource.Files = []string{exPath}
			}
		}
	case "custom":
		src := strings.TrimSpace(a.userSourceInput.Value())
		if src != "" {
			p.UserSource.Files = []string{src}
		}
		for sym, cb := range a.checks {
			p.Features[sym] = cb.Value()
		}
	}
	return p
}

// applyProject sets GUI state from a loaded Project.
func (a *appState) applyProject(p *project.Project) {
	mode := p.StudioMode
	if mode != "examples" && mode != "custom" {
		// Infer from contents: a project pointing at user-provided sources
		// is a custom project; one naming an example is an example project.
		// This keeps hand-written TOMLs (which won't have studio_mode set)
		// from being misclassified as "examples" and silently ignoring their
		// user_source list.
		switch {
		case p.ExampleName != "":
			mode = "examples"
		case len(p.UserSource.Files) > 0:
			mode = "custom"
		default:
			mode = "examples"
		}
	}
	// Restore bootloader dropdown from the loaded project.
	if a.bootloaderDropdown != nil {
		switch {
		case p.Bootloader == nil:
			a.bootloaderDropdown.SetSelected(0)
		case p.Bootloader.TSBL == "bypass":
			a.bootloaderDropdown.SetSelected(1)
		case p.Bootloader.TSBL == "ab":
			a.bootloaderDropdown.SetSelected(2)
		default:
			a.bootloaderDropdown.SetSelected(0)
		}
	}
	a.mode.Set(mode)
	if a.modeTabBar != nil {
		if mode == "custom" {
			a.modeTabBar.SetSelected(1)
		} else {
			a.modeTabBar.SetSelected(0)
		}
	}
	if p.Name != "" {
		a.nameInput.SetValue(p.Name)
	}
	if p.Target != "" {
		for i, n := range a.targetNames {
			if n == p.Target {
				a.targetDropdown.SetSelected(i)
				break
			}
		}
	}
	if p.Layout == "sram" {
		a.layoutDropdown.SetSelected(1)
	} else {
		a.layoutDropdown.SetSelected(0)
	}
	// Also persist the loaded layout into the per-mode store so a subsequent
	// tab toggle round-trip doesn't reset back to the static default.
	switch mode {
	case "examples":
		a.examplesLayout = p.Layout
	case "custom":
		a.customLayout = p.Layout
	}
	switch mode {
	case "examples":
		// Restore example selection by name.
		if a.exampleDropdown != nil && p.ExampleName != "" {
			// Clear filter so the named entry is visible in the dropdown items.
			if a.exampleFilter != nil {
				a.exampleFilter.SetValue("")
			}
			a.applyExampleFilter()
			for i, n := range a.allExampleNames {
				if n == p.ExampleName {
					a.exampleDropdown.SetSelected(i)
					break
				}
			}
		}
	case "custom":
		if len(p.UserSource.Files) > 0 {
			a.userSourceInput.SetValue(p.UserSource.Files[0])
		}
		// Restore feature toggles — only for symbols the TOML explicitly
		// mentions. A missing key returns false from p.Features which
		// would otherwise wrongly *override* catalog defaults (e.g. STARTUP
		// is default=true; if the TOML doesn't list it, we must not flip
		// it off). The comma-ok lookup distinguishes "absent" from "false".
		for sym, cb := range a.checks {
			if v, ok := p.Features[sym]; ok {
				cb.SetValue(v)
			}
		}
	}
}

// leftPane dispatches to the right side-content based on mode. In Examples
// mode it shows the current example's source preview / info; in Custom mode
// it shows the feature checkboxes + user source path input.
func (a *appState) leftPane() ui.View {
	if a.mode.Get() == "custom" {
		return a.featurePane()
	}
	return a.exampleInfoPane()
}

// exampleInfoPane shows the currently-picked example's filename and the first
// ~12 lines of its source so users can see what the demo does without leaving
// the GUI. Scroll-wrapped so a long header doesn't blow up the layout.
func (a *appState) exampleInfoPane() ui.View {
	name, path := a.currentExample()
	if name == "" {
		return ui.Card(
			ui.VStack(
				ui.Text("Examples").Subtitle(),
				ui.Divider(),
				ui.Text("(no example selected)").Small(),
			).Spacing(ui.SpaceSm),
		).Elevation(ui.ElevationLow).CornerRadius(ui.RadiusMd)
	}
	preview := readExampleHeader(path)
	rows := []ui.View{
		ui.Text("Example").Subtitle(),
		ui.Divider(),
		ui.Text(name).Bold(),
		ui.Text(path).Small(),
		ui.Spacer(),
		ui.Text("Source preview").Small().Bold(),
	}
	for _, line := range preview {
		rows = append(rows, ui.Text(line).Small())
	}
	return ui.Card(
		ui.Scroll(ui.VStack(rows...).Spacing(ui.SpaceXS)),
	).Elevation(ui.ElevationLow).CornerRadius(ui.RadiusMd)
}

// readExampleHeader returns the first 12 lines of the .S file (assumed to be
// a comment header). Best-effort; returns a "(could not read)" line on error.
func readExampleHeader(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{"(could not read " + path + ")"}
	}
	all := strings.Split(string(b), "\n")
	if len(all) > 12 {
		all = all[:12]
	}
	return all
}

// toolsRow shows USB-access (udev) status on Linux plus the live BOOTSEL
// board badge. v1 uses in-tree rpasmboot — no external tool install, so the
// only setup step a user might need is the udev rule.
func (a *appState) toolsRow() ui.View {
	board := a.boardSt.Get()

	row := []ui.View{}
	if runtime.GOOS == "linux" {
		if a.udevOK.Get() {
			row = append(row, ui.Text("udev: rules installed").Small())
		} else {
			row = append(row, ui.Text("udev: rules missing — run `rpasm doctor` for install command").Small())
		}
		row = append(row, ui.Spacer())
	}
	if board.InBootsel {
		row = append(row, ui.Text("Board: in BOOTSEL @ "+board.Mountpoint).Small().Bold())
	} else {
		row = append(row, ui.Text("Board: not detected").Small())
	}
	if a.querying.Get() {
		row = append(row, ui.Button("Querying...").Outline())
	} else {
		row = append(row, ui.Button("Query slots").OnClick(a.onQuerySlots))
	}
	header := ui.HStack(row...).Spacing(ui.SpaceMd).Center()

	// Slot-status panel below the row, shown only when we have data or an
	// error from a recent query.
	slots := a.slotInfo.Get()
	errMsg := a.slotErr.Get()
	if len(slots) == 0 && errMsg == "" {
		return header
	}
	var panel []ui.View
	if errMsg != "" {
		panel = append(panel, ui.Text("Slot query: "+errMsg).Small())
	}
	for _, s := range slots {
		panel = append(panel, ui.Text(formatSlot(s)).Small())
	}
	return ui.VStack(header, ui.VStack(panel...).Spacing(ui.SpaceXS)).Spacing(ui.SpaceSm)
}

func formatSlot(s flash.SlotInfo) string {
	if !s.Valid {
		return fmt.Sprintf("  slot %s @ 0x%08x: (empty)", s.Name, s.Base)
	}
	return fmt.Sprintf("  slot %s @ 0x%08x: status=%s seq=%d payload=%d B",
		s.Name, s.Base, flash.StatusName(s.Footer.Status), s.Footer.Seq, s.Footer.PayloadSize)
}

func (a *appState) onQuerySlots() {
	if a.querying.Get() {
		return
	}
	a.querying.Set(true)
	a.slotErr.Set("")
	go func() {
		defer a.querying.Set(false)
		slots, err := flash.ReadBootInfo()
		if err != nil {
			a.slotErr.Set(err.Error())
			a.slotInfo.Set(nil)
			a.log("[bootinfo] ERROR: " + err.Error())
			return
		}
		a.slotInfo.Set(slots)
		for _, s := range slots {
			a.log("[bootinfo] " + strings.TrimSpace(formatSlot(s)))
		}
	}()
}

// detectUdevRule reports whether our udev rule file exists. Always true on
// non-Linux hosts (no udev to install).
func detectUdevRule() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	_, err := os.Stat(udevRulePath)
	return err == nil
}

// topBar adapts to window width:
//   - wide  (>=1300 dp): single row — title | spacer | config | actions
//   - narrow (<1300 dp): two rows
//       row 1: title | spacer | actions     (Build/Flash always reachable)
//       row 2: config controls               (dropdowns/inputs, may stack tightly)
//
// The control widgets are captured once per frame and reused in both layouts
// so click handlers stay connected regardless of which row they end up in.
func (a *appState) topBar() ui.View {
	config := a.topBarConfig()
	actions := a.topBarActions()
	return ui.Responsive(
		ui.At(0, func() ui.View {
			rowTitle := append([]ui.View{
				ui.Text("rp-asm Studio").Title(),
				ui.Spacer(),
			}, actions...)
			return ui.VStack(
				ui.HStack(rowTitle...).Spacing(ui.SpaceMd).Center(),
				ui.HStack(config...).Spacing(ui.SpaceMd).Center(),
			).Spacing(ui.SpaceSm)
		}),
		ui.At(1300, func() ui.View {
			row := []ui.View{ui.Text("rp-asm Studio").Title(), ui.Spacer()}
			row = append(row, config...)
			row = append(row, actions...)
			return ui.HStack(row...).Spacing(ui.SpaceMd).Center()
		}),
	)
}

// topBarConfig is the dropdowns/inputs portion of the top bar (mode-dependent).
//   - examples: Example label + filter + dropdown + Target + Layout
//   - custom:   Target + Layout + Name
func (a *appState) topBarConfig() []ui.View {
	mode := a.mode.Get()
	out := []ui.View{}
	if mode == "examples" && a.exampleDropdown != nil {
		a.applyExampleFilter()
		out = append(out,
			ui.Text("Example").Small(),
			a.exampleFilter,
			a.exampleDropdown,
		)
	}
	out = append(out,
		ui.Text("Target").Small(),
		a.targetDropdown,
		ui.Text("Layout").Small(),
		a.layoutDropdown,
	)
	if mode == "custom" {
		out = append(out,
			ui.Text("Bootloader").Small(),
			a.bootloaderDropdown,
			ui.Text("Name").Small(),
			a.nameInput,
		)
	}
	return out
}

// currentBootloader returns the project.Bootloader value implied by the
// bootloader dropdown. Returns nil when "(no bootloader)" is selected so
// the build engine produces a bare UF2; non-nil produces the firmware chain.
func (a *appState) currentBootloader() *project.Bootloader {
	if a.bootloaderDropdown == nil {
		return nil
	}
	switch a.bootloaderDropdown.SelectedText() {
	case "bypass":
		return &project.Bootloader{TSBL: "bypass"}
	case "ab":
		return &project.Bootloader{TSBL: "ab"}
	default:
		return nil
	}
}

// topBarActions is the action-button portion of the top bar: Build, Flash.
// Always on the same row as the title so they're never clipped off-screen.
func (a *appState) topBarActions() []ui.View {
	building := a.building.Get()
	flashing := a.flashing.Get()
	haveUf2 := a.haveBuiltUf2()
	return []ui.View{
		ui.IfElse(building,
			ui.Button("Building...").Outline(),
			ui.Button("Build").OnClick(a.onBuild),
		),
		ui.IfElse(flashing,
			ui.Button("Flashing...").Outline(),
			ui.IfElse(haveUf2,
				ui.Button("Flash").OnClick(a.onFlash),
				ui.Button("Flash (build first)").Outline().OnClick(a.onFlashDisabled),
			),
		),
	}
}

// selectedModulesView renders the currently-checked modules as a wrapping
// row of badges so the user can see what the next Build will pull in
// without scrolling through the full Features checkbox grid. Updates each
// frame because checkbox state can change between renders.
func (a *appState) selectedModulesView() ui.View {
	const perRow = 6
	var syms []string
	for _, m := range a.modules {
		if cb, ok := a.checks[m.Symbol]; ok && cb.Value() {
			syms = append(syms, m.Symbol)
		}
	}
	header := ui.Text(fmt.Sprintf("Selected modules (%d):", len(syms))).Small().Bold()
	if len(syms) == 0 {
		return ui.VStack(
			header,
			ui.Text("(none — user source will be built standalone)").Small(),
		).Spacing(ui.SpaceXS)
	}
	var rows []ui.View
	rows = append(rows, header)
	for i := 0; i < len(syms); i += perRow {
		end := i + perRow
		if end > len(syms) {
			end = len(syms)
		}
		row := make([]ui.View, 0, end-i)
		for _, s := range syms[i:end] {
			row = append(row, ui.Badge(s).Secondary())
		}
		rows = append(rows, ui.HStack(row...).Spacing(ui.SpaceXS))
	}
	return ui.VStack(rows...).Spacing(ui.SpaceXS)
}

func (a *appState) featurePane() ui.View {
	sections := []ui.View{
		ui.Text("Source").Subtitle(),
		ui.Divider(),
		ui.Text("Path to your .S file (resolved relative to SDK root):").Small(),
		a.userSourceInput,
	}
	if strings.TrimSpace(a.userSourceInput.Value()) != "" {
		sections = append(sections, a.selectedModulesView())
	}
	sections = append(sections,
		ui.Spacer(),
		ui.Text("Features").Subtitle(),
		ui.Divider(),
	)
	for _, cat := range a.categories {
		sections = append(sections, ui.Text(prettyCategory(cat)).Bold())
		for _, m := range a.modules {
			if m.Category != cat {
				continue
			}
			sections = append(sections, a.checks[m.Symbol])
		}
		sections = append(sections, ui.Spacer())
	}
	return ui.Card(
		ui.Scroll(
			ui.VStack(sections...).Spacing(ui.SpaceXS),
		),
	).Elevation(ui.ElevationLow).CornerRadius(ui.RadiusMd)
}

func (a *appState) outputPane() ui.View {
	problems := a.problems.Get()
	mem := a.memory.Get()
	active := a.activeTab.Get()
	if active == "" {
		active = "output"
	}
	var body ui.View
	switch active {
	case "problems":
		body = a.problemsBody(problems)
	case "memory":
		body = a.memoryBody(mem)
	default:
		body = a.outputBody()
	}
	return ui.Card(
		ui.VStack(
			ui.Themed(func(th *theme.Theme) ui.View {
				return ui.Text(a.status.Get()).Caption().Color(th.Palette.Primary)
			}),
			a.tabStrip(active, len(problems), mem != nil || a.bootloader.Get() != nil),
			ui.Divider(),
			body,
		).Spacing(ui.SpaceSm),
	).Elevation(ui.ElevationLow).CornerRadius(ui.RadiusMd)
}

func (a *appState) tabStrip(active string, problemCount int, haveMemory bool) ui.View {
	outputBtn := ui.Button("Output").OnClick(func() { a.activeTab.Set("output") })
	if active != "output" {
		outputBtn = outputBtn.Outline()
	}
	problemsLabel := "Problems"
	if problemCount > 0 {
		problemsLabel = fmt.Sprintf("Problems (%d)", problemCount)
	}
	problemsBtn := ui.Button(problemsLabel).OnClick(func() { a.activeTab.Set("problems") })
	if active != "problems" {
		problemsBtn = problemsBtn.Outline()
	}
	memoryBtn := ui.Button("Memory").OnClick(func() { a.activeTab.Set("memory") })
	if active != "memory" {
		memoryBtn = memoryBtn.Outline()
	}
	if !haveMemory {
		// Keep the button visible but neuter it — clicking before a build
		// drops the user on an empty pane, which is fine.
		memoryBtn = memoryBtn.Disabled()
	}
	return ui.HStack(outputBtn, problemsBtn, memoryBtn).Spacing(ui.SpaceSm)
}

func (a *appState) memoryBody(mem *build.MemoryUsage) ui.View {
	bl := a.bootloader.Get()
	empty := (mem == nil || (len(mem.Regions) == 0 && len(mem.Sections) == 0)) && bl == nil
	if empty {
		return ui.Scroll(ui.VStack(
			ui.Text("(memory info not available — run Build first)").Small(),
		))
	}
	rows := []ui.View{}
	if mem != nil && len(mem.Regions) > 0 {
		rows = append(rows, ui.Text("Regions").Bold())
		for _, r := range mem.Regions {
			rows = append(rows, ui.Text(formatRegion(r)).Small())
		}
		rows = append(rows, ui.Spacer())
	}
	if bl != nil && len(bl.Stages) > 0 {
		rows = append(rows, ui.Text("Bootloader chain").Bold())
		for _, s := range bl.Stages {
			rows = append(rows, ui.Text(formatStage(s)).Small())
		}
		rows = append(rows, ui.Spacer())
	}
	if mem != nil && len(mem.Sections) > 0 {
		rows = append(rows, ui.Text("Sections").Bold())
		for _, s := range mem.Sections {
			rows = append(rows, ui.Text(formatSection(s)).Small())
		}
	}
	return ui.Scroll(ui.VStack(rows...).Spacing(ui.SpaceXS))
}

func formatRegion(r build.MemoryRegion) string {
	return fmt.Sprintf("  %-10s %10s / %-10s  %5.2f%%",
		r.Name+":", humanBytes(r.Used), humanBytes(r.Size), r.Percent())
}

func formatStage(s build.BootloaderStage) string {
	return fmt.Sprintf("  %-10s %10s / %-10s  %5.2f%%   @ 0x%08x",
		s.Name+":", humanBytes(s.Used), humanBytes(s.Capacity), s.Percent(), s.Base)
}

func formatSection(s build.MemorySection) string {
	return fmt.Sprintf("  %-22s %10s  @ 0x%08x", s.Name, humanBytes(s.Size), s.Addr)
}

func humanBytes(n uint64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.2f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.2f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func (a *appState) outputBody() ui.View {
	lines := a.logLines.Get()
	logViews := make([]ui.View, 0, len(lines)+1)
	if len(lines) == 0 {
		logViews = append(logViews, ui.Text("(no output yet — click Build)").Small())
	}
	for _, line := range lines {
		logViews = append(logViews, ui.Text(line).Small())
	}
	return ui.Scroll(ui.VStack(logViews...).Spacing(ui.SpaceXS))
}

func (a *appState) problemsBody(problems []Problem) ui.View {
	if len(problems) == 0 {
		return ui.Scroll(ui.VStack(
			ui.Text("(no problems — Build hasn't surfaced any errors or warnings)").Small(),
		))
	}
	rows := make([]ui.View, 0, len(problems))
	for _, p := range problems {
		rows = append(rows, a.problemRow(p))
	}
	return ui.Scroll(ui.VStack(rows...).Spacing(ui.SpaceXS))
}

func (a *appState) problemRow(p Problem) ui.View {
	// Strip a leading SDK-root prefix from the file path so long absolute
	// paths don't dominate the row. Keep absolute if it's outside the root.
	display := p.File
	parent := filepath.Dir(a.root)
	if rel, err := filepath.Rel(parent, p.File); err == nil && !strings.HasPrefix(rel, "..") {
		display = rel
	}
	badge := "E"
	switch p.Severity {
	case "warning":
		badge = "W"
	case "fatal":
		badge = "F"
	case "note":
		badge = "N"
	}
	header := fmt.Sprintf("%s  %s:%d", badge, display, p.Line)
	return ui.VStack(
		ui.Text(header).Bold().Small(),
		ui.Text("   "+p.Message).Small(),
	).Spacing(ui.SpaceXS)
}

func (a *appState) onBuild() {
	if a.building.Get() {
		return
	}
	a.building.Set(true)
	a.logLines.Set([]string{})
	a.problems.Set([]Problem{})
	a.memory.Set(nil)
	a.bootloader.Set(nil)
	a.activeTab.Set("output")
	a.status.Set("Building...")

	go func() {
		defer a.building.Set(false)
		a.runBuild()
	}()
}

func (a *appState) runBuild() {
	mode := a.mode.Get()
	var name, src string
	switch mode {
	case "custom":
		name = strings.TrimSpace(a.nameInput.Value())
		src = strings.TrimSpace(a.userSourceInput.Value())
		if src == "" {
			a.status.Set("Source path required.")
			a.log("ERROR: Custom mode needs a source .S file path (left pane → Source).")
			return
		}
		if name == "" {
			// Derive project name from source basename if Name left blank.
			name = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		}
	default: // "examples"
		exName, exPath := a.currentExample()
		if exPath == "" {
			a.status.Set("No example selected.")
			a.log("ERROR: pick an example from the dropdown first.")
			return
		}
		name = exName
		src = exPath
	}
	proj := &project.Project{
		Name:     name,
		Target:   a.currentTarget(),
		Layout:   a.currentLayout(),
		Features: map[string]bool{},
		UserSource: project.UserSource{
			Files: []string{src},
		},
		Bootloader: a.currentBootloader(),
	}
	if proj.Name == "" {
		proj.Name = "blinky"
	}
	for sym, cb := range a.checks {
		proj.Features[sym] = cb.Value()
	}

	a.log(fmt.Sprintf("project: %s  target: %s  layout: %s", proj.Name, proj.Target, proj.Layout))

	res, err := project.Resolve(proj, a.catalog)
	if err != nil {
		a.status.Set("Resolve failed.")
		a.log("ERROR: " + err.Error())
		return
	}
	a.log(fmt.Sprintf("modules: %d enabled, %d sources", len(res.Modules), len(res.Sources)))

	tc, err := build.Detect(res.Target.ToolchainPrefix)
	if err != nil {
		a.status.Set("Toolchain missing.")
		a.log("ERROR: " + err.Error())
		return
	}
	a.log("toolchain: " + tc.As)

	// SDK-root build tree (parent of studio module root). Studio and the
	// Makefile share this directory; per-project subdirs prevent collision.
	outDir := filepath.Join(filepath.Dir(a.root), "build", proj.Name)
	logWriter := &lineLogger{
		emit:      a.log,
		onProblem: a.addProblem,
	}
	result, err := build.Build(&build.Options{
		Resolved:  res,
		Root:      a.root,
		OutDir:    outDir,
		Toolchain: tc,
		Stdout:    logWriter,
		Stderr:    logWriter,
	})
	logWriter.flush()
	if err != nil {
		a.status.Set("Build failed.")
		a.log("ERROR: " + err.Error())
		// If problems were parsed, surface the Problems tab on failure so the
		// user lands on the structured errors rather than the raw scrollback.
		if len(a.problems.Get()) > 0 {
			a.activeTab.Set("problems")
		}
		return
	}
	a.log("ELF " + result.Elf)
	a.log("BIN " + result.Bin)
	a.log("UF2 " + result.Uf2)
	a.logMemorySummary(result)
	a.memory.Set(result.Memory)
	a.bootloader.Set(result.Bootloader)
	a.status.Set("Build succeeded.")
}

// logMemorySummary dumps the same content the Memory tab renders so it ends
// up in the (stderr-mirrored) log, making the values copyable.
func (a *appState) logMemorySummary(r *build.Result) {
	if r.Memory != nil && len(r.Memory.Regions) > 0 {
		a.log("Memory regions:")
		for _, reg := range r.Memory.Regions {
			a.log(fmt.Sprintf("  %-10s %10s / %-10s  %5.2f%%",
				reg.Name+":", humanBytes(reg.Used), humanBytes(reg.Size), reg.Percent()))
		}
	}
	if r.Bootloader != nil && len(r.Bootloader.Stages) > 0 {
		a.log("Bootloader chain:")
		for _, s := range r.Bootloader.Stages {
			a.log(fmt.Sprintf("  %-10s %10s / %-10s  %5.2f%%   @ 0x%08x",
				s.Name+":", humanBytes(s.Used), humanBytes(s.Capacity), s.Percent(), s.Base))
		}
	}
	if r.Memory != nil && len(r.Memory.Sections) > 0 {
		a.log("Sections:")
		for _, sec := range r.Memory.Sections {
			a.log(fmt.Sprintf("  %-22s %10s  @ 0x%08x", sec.Name, humanBytes(sec.Size), sec.Addr))
		}
	}
}

func (a *appState) onFlashDisabled() {
	a.status.Set("Click Build first — no UF2 yet.")
	a.log("flash: no UF2 has been built yet in this session. Click Build, then Flash.")
}

func (a *appState) onFlash() {
	if a.flashing.Get() || a.building.Get() {
		return
	}
	uf2 := a.expectedUf2()
	if uf2 == "" {
		return
	}
	// Re-check on disk: dropdown could have changed between render and click,
	// or someone could have rm'd the file.
	if _, err := os.Stat(uf2); err != nil {
		a.status.Set("UF2 missing — click Build first.")
		a.log("flash: expected " + uf2 + " — not on disk; click Build first.")
		return
	}
	a.flashing.Set(true)
	a.status.Set("Flashing " + filepath.Base(uf2) + "...")
	a.log("flashing " + uf2)
	go func() {
		defer a.flashing.Set(false)
		w := &lineLogger{emit: a.log}
		result, err := flash.Flash(&flash.Options{
			Uf2Path: uf2,
			Log:     a.log,
			Stdout:  w,
			Stderr:  w,
		})
		w.flush()
		if err != nil {
			a.status.Set("Flash failed.")
			a.log("ERROR: " + err.Error())
			return
		}
		a.log(fmt.Sprintf("flashed via %s (%s)", result.Method, result.Target))
		a.status.Set("Flashed " + filepath.Base(uf2) + ".")
	}()
}

func (a *appState) log(s string) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	a.logLines.Update(func(prev []string) []string {
		return append(prev, s)
	})
	// Mirror to stderr so the terminal that launched the GUI has a
	// copy-pasteable record of everything that appears in the Output
	// panel. Avoids needing selectable widgets just to share logs.
	fmt.Fprintln(os.Stderr, s)
}

func (a *appState) addProblem(p Problem) {
	a.problems.Update(func(prev []Problem) []Problem {
		return append(prev, p)
	})
}

// currentExample returns (displayName, sourcePath) for the picked example, or
// ("", "") if no examples are available. The selected text is resolved against
// the full universe (allExampleNames), so filtering doesn't lose the mapping.
func (a *appState) currentExample() (string, string) {
	if a.exampleDropdown == nil || len(a.allExamplePaths) == 0 {
		return "", ""
	}
	sel := a.exampleDropdown.SelectedText()
	if sel == "" {
		return "", ""
	}
	for i, n := range a.allExampleNames {
		if n == sel {
			return n, a.allExamplePaths[i]
		}
	}
	return "", ""
}

// applyExampleFilter recomputes the dropdown's visible items from the current
// filter input. Called every frame from topBarControls(). Cheap (49 strings).
// We compare against lastFilterApplied to avoid stomping on the widget's items
// when nothing changed (which would needlessly reset Clickable hover state).
func (a *appState) applyExampleFilter() {
	if a.exampleDropdown == nil || a.exampleFilter == nil {
		return
	}
	q := strings.ToLower(strings.TrimSpace(a.exampleFilter.Value()))
	if q == a.lastFilterApplied {
		return
	}
	a.lastFilterApplied = q

	var items []string
	if q == "" {
		items = append(items, a.allExampleNames...)
	} else {
		for _, n := range a.allExampleNames {
			if strings.Contains(strings.ToLower(n), q) {
				items = append(items, n)
			}
		}
	}
	// Preserve the user's current selection by name if it survives the filter.
	prevSel := a.exampleDropdown.SelectedText()
	a.exampleDropdown.SetItems(items)
	newIdx := -1
	for i, n := range items {
		if n == prevSel {
			newIdx = i
			break
		}
	}
	if newIdx >= 0 {
		a.exampleDropdown.SetSelected(newIdx)
	} else if len(items) > 0 {
		a.exampleDropdown.SetSelected(0)
	} else {
		a.exampleDropdown.SetSelected(-1)
	}
}

func (a *appState) currentTarget() string {
	if len(a.targetNames) == 0 {
		return ""
	}
	t := a.targetDropdown.SelectedText()
	if t == "" {
		return a.targetNames[0]
	}
	return t
}

func (a *appState) currentLayout() string {
	l := a.layoutDropdown.SelectedText()
	if l == "" {
		return "flash"
	}
	return l
}

func prettyCategory(c string) string {
	switch c {
	case "system":
		return "System"
	case "peripherals":
		return "Peripherals"
	default:
		if c == "" {
			return "Other"
		}
		return c
	}
}

func studioRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		fatal("getwd: %v", err)
	}
	// Walk up first — covers the common case where CWD is inside studio/.
	for d := cwd; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(d, "catalog")); err == nil {
				return d
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	// Fall back to descending one level: launched from the SDK root
	// (`go run ./studio/cmd/rpasm-studio` from rp-asm/) where catalog
	// lives at studio/catalog.
	if _, err := os.Stat(filepath.Join(cwd, "studio", "catalog")); err == nil {
		return filepath.Join(cwd, "studio")
	}
	return cwd
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// lineLogger is an io.Writer that splits incoming bytes on '\n', calls emit
// for each completed line, and additionally feeds each line through a
// problem-parser whose hits go to onProblem. Both callbacks are optional.
type lineLogger struct {
	emit      func(string)
	onProblem func(Problem)
	buf       []byte
	mu        sync.Mutex
}

func (l *lineLogger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := indexNewline(l.buf)
		if i < 0 {
			break
		}
		line := string(l.buf[:i])
		l.buf = l.buf[i+1:]
		l.handle(line)
	}
	return len(p), nil
}

func (l *lineLogger) flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) > 0 {
		l.handle(string(l.buf))
		l.buf = l.buf[:0]
	}
}

func (l *lineLogger) handle(line string) {
	if line == "" {
		return
	}
	if l.emit != nil {
		l.emit(line)
	}
	if l.onProblem != nil {
		if p, ok := parseProblem(line); ok {
			l.onProblem(p)
		}
	}
}

func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}

// problemRE matches GNU as / gcc style diagnostics:
//
//	<file>:<line>: <Severity>: <message>
//
// File can be any non-empty string (no embedded colons in practice for our
// .S files; if needed later we can tighten this). Severity is case-insensitive.
var problemRE = regexp.MustCompile(`^(.+?):(\d+):\s*(Error|Warning|Fatal error|Note|error|warning|fatal error|note):\s*(.+)$`)

func parseProblem(line string) (Problem, bool) {
	m := problemRE.FindStringSubmatch(line)
	if m == nil {
		return Problem{}, false
	}
	lineNo, err := strconv.Atoi(m[2])
	if err != nil {
		return Problem{}, false
	}
	sev := strings.ToLower(m[3])
	if sev == "fatal error" {
		sev = "fatal"
	}
	return Problem{
		File:     m[1],
		Line:     lineNo,
		Severity: sev,
		Message:  m[4],
	}, true
}

// loadExamples returns the example dropdown contents as parallel slices of
// display names and absolute source paths.
//
// Index 0 is always the canonical hardware blinky from <parent>/src/main.S —
// the default the GUI selects on startup. Indices 1..N are the contents of
// <parent>/examples/*.S, sorted by basename. <parent> is the SDK root that
// holds Makefile, src/, include/, examples/.
//
// Note: blinky_v01.S in examples/ is intentionally not-flash-bootable (it's a
// Unicorn MMIO trace regression image; see commit 27bc9d3) and would hang on
// real hardware, so we deliberately do NOT default to it.
//
// Returns ([head], [head-path]) with no examples if examples/ is missing.
func loadExamples(studioRoot string) ([]string, []string) {
	parent := filepath.Dir(studioRoot)
	names := []string{"blinky"}
	paths := []string{filepath.Join(parent, "src", "main.S")}

	exDir := filepath.Join(parent, "examples")
	entries, err := os.ReadDir(exDir)
	if err != nil {
		return names, paths
	}
	type pair struct{ name, path string }
	var pairs []pair
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".S" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".S")
		pairs = append(pairs, pair{name, filepath.Join(exDir, e.Name())})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].name < pairs[j].name })
	for _, p := range pairs {
		names = append(names, p.name)
		paths = append(paths, p.path)
	}
	return names, paths
}
