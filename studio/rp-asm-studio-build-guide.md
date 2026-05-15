# rp-asm Studio — Build Guide

A practical blueprint for building rp-asm Studio: the visual configurator and build engine for the rp-asm assembly SDK.

## What we're building

A desktop app that lets users configure, build, and flash assembly-language firmware for RP2040 and RP2350 targets without ever touching Make, CMake, or Kconfig directly. The GUI *is* the build system from the user's perspective. Kconfig is used internally as the catalog format for contributors authoring rp-asm modules.

Sibling product to Pingo. Shares the ImmyGo framework, visual language, and AI-assist patterns.

## Architecture

Five layers, each independently testable. Build them in this order.

```
┌─────────────────────────────────────────────┐
│  Layer 5 — GUI (Gio panels)                 │  Configure / Build log / Catalog source
├─────────────────────────────────────────────┤
│  Layer 4 — Project state                    │  Selected features, target, pin maps
├─────────────────────────────────────────────┤
│  Layer 3 — Build engine                     │  Invokes as / ld / elf2uf2
├─────────────────────────────────────────────┤
│  Layer 2 — Catalog parser                   │  Parses .kconfig files, evaluates expressions
├─────────────────────────────────────────────┤
│  Layer 1 — Catalog files (.kconfig)         │  Authored by contributors, ships with app
└─────────────────────────────────────────────┘
```

The bottom three layers are headless and have no UI dependencies. They can be exercised from a CLI or test harness long before any window opens. This is the single most important architectural decision in the project — protect it.

## Tech stack

| Concern | Choice | Rationale |
|---|---|---|
| Language | Go | Matches existing Amken stack; Pingo and ImmyGo are Go |
| GUI framework | Gio (via ImmyGo) | Reuse, consistency, single-binary distribution |
| Catalog format | Kconfig subset | Familiar to embedded contributors, well-defined semantics |
| Parser | Hand-written Go parser | ~500 LOC; existing libs are Python (kconfiglib) or coupled to Linux build |
| Build orchestration | Go `os/exec` + mtime tracking | Assembly graphs are shallow; no need for Ninja in v1 |
| Toolchain | User-supplied on PATH (v1), bundled (v2) | Keep v1 small; bundle GNU arm-none-eabi-* later |
| Project file | TOML | Diffable in git, editable by power users |
| Catalog distribution | Embedded via `embed.FS` | Catalog ships inside the binary; no install-time setup |

## Project structure

```
rp-asm-studio/
├── cmd/
│   ├── rpasm-studio/        Main GUI app entry point
│   └── rpasm/               Headless CLI (build, flash, validate) — same engine, no GUI
├── internal/
│   ├── catalog/             Layer 2 — Kconfig parser, AST, expression evaluator
│   │   ├── lexer.go
│   │   ├── parser.go
│   │   ├── ast.go
│   │   └── eval.go
│   ├── project/             Layer 4 — project state, TOML load/save, validation
│   │   ├── state.go
│   │   ├── toml.go
│   │   └── validate.go
│   ├── build/               Layer 3 — toolchain invocation, mtime tracking, UF2 generation
│   │   ├── engine.go
│   │   ├── toolchain.go
│   │   └── uf2.go
│   └── ui/                  Layer 5 — Gio panels
│       ├── configure.go
│       ├── buildlog.go
│       ├── catalog.go       Developer mode source view
│       └── theme.go         Reuse from ImmyGo
├── catalog/                 Layer 1 — Kconfig files, embedded via go:embed
│   ├── system/
│   │   ├── boot.kconfig
│   │   └── crystal.kconfig
│   ├── peripherals/
│   │   ├── gpio.kconfig
│   │   ├── uart.kconfig
│   │   ├── spi.kconfig
│   │   └── ...
│   └── targets/
│       ├── rp2040.kconfig
│       ├── rp2350-arm.kconfig
│       └── rp2350-riscv.kconfig
├── asm/                     The actual rp-asm assembly sources, embedded
│   ├── system/
│   ├── peripherals/
│   └── linker/
└── testdata/                Golden project files for regression tests
```

The `cmd/rpasm` headless CLI is non-negotiable. It exercises Layers 1-4 without the GUI, gives you CI testability, and gives power users a scriptable interface. Build it first.

## Phased implementation

Ship something usable at every phase. Don't build all five layers in parallel.

### Phase 0.1 — Catalog parser (week 1-2)

Goal: parse a `.kconfig` file into an AST and walk it.

- Implement the lexer for the Kconfig subset you actually need: `config`, `bool`, `int`, `string`, `choice`, `endchoice`, `default`, `range`, `depends on`, `select`, `help`, `if`, `endif`, `menu`, `endmenu`, `source`.
- Skip the parts you don't need: `mainmenu`, `comment`, `option`, `prompt` (use the inline string instead), `imply`, `visible if`.
- Build the AST as plain Go structs. No reflection, no codegen.
- Implement the expression evaluator for `depends on` / `default if` / `select if` — it's a tiny boolean language: `&&`, `||`, `!`, `=`, `!=`, parens, symbol refs, literal `y` / `n` / `m` (skip `m`, you don't need tristate).
- Write golden tests against ~20 hand-crafted `.kconfig` snippets covering each construct.

Done when: you can load `catalog/peripherals/uart.kconfig` and print the resulting symbol table.

### Phase 0.2 — Project state and CLI (week 3)

Goal: a working `rpasm` command that resolves a project file.

- Define the project state: target, map of symbol → value, pin assignments.
- Implement TOML load/save.
- Implement constraint resolution: given a partial state, propagate `select` chains, evaluate `depends on`, fill in defaults, flag conflicts.
- CLI commands: `rpasm validate <project.rpasm>`, `rpasm symbols <project.rpasm>` (dump resolved config).

Done when: you can hand-write a `blink-led.rpasm` TOML file and have `rpasm validate` report exactly which symbols are enabled.

### Phase 0.3 — Build engine (week 4-5)

Goal: `rpasm build <project.rpasm>` produces a working `.uf2`.

- Translate resolved config → list of `.s` files to assemble (each catalog symbol declares its source files).
- Implement the toolchain wrapper: detect `arm-none-eabi-as` / `riscv32-unknown-elf-as` on PATH, run with the right flags per target.
- Generate the linker script from a template based on target memory map and selected features.
- Invoke `ld`, then a Go-native UF2 generator (UF2 format is trivial: 512-byte blocks with a known header).
- Track mtimes for incremental builds — keep a `build/.cache.json` mapping source path → hash + output path.

Done when: `rpasm build` produces a `.uf2` that boots on a real Pico 2 and blinks the LED.

### Phase 0.4 — Minimal GUI (week 6-8)

Goal: the configure view from the mockup, end-to-end.

- Bootstrap a Gio window using ImmyGo's theme primitives.
- Build the feature tree component (reuse Pingo's tree pattern).
- Build the detail panel that renders based on selected symbol type (bool → toggle, int → numeric input, choice → radio group, string → text input).
- Wire up the Build button to call the Layer 3 engine in a goroutine; stream output to a status area.
- Add `present_files`-style flash action that runs `picotool load` or copies UF2 to mounted RPI-RP2 drive.

Done when: a user can launch the app, pick a target, toggle UART on, click Build, and get a UF2.

### Phase 0.5 — Build log view (week 9)

Goal: the second view from the mockup.

- Tab strip: Output / Problems / Memory.
- Output: streaming append-only text view with monospace font.
- Problems: parse assembler error output (`file.s:line:col: error: ...`), present as clickable rows.
- Memory: parse `ld --print-memory-usage` output, render the segmented bar.

### Phase 0.6 — Catalog source view (developer mode, week 10)

Goal: the third view from the mockup.

- File watcher on `./catalog/` directory; auto-reload on save.
- Source pane: read-only, with syntax highlighting (use a small handwritten lexer pass).
- Preview pane: same components as the user-facing detail panel, rendered from the just-parsed AST.
- Hide behind a Developer mode toggle in app settings.

### Phase 1.0 — Polish and ship (week 11-12)

- Code signing for macOS and Windows binaries (Amken cert).
- Update channel — embed a version check that pings a static `latest.json` on the Amken site.
- Crash reporter that writes local logs to `~/.rpasm-studio/crashes/` and offers to open them; no telemetry.
- Sample projects bundled in the binary, accessible from a File → New from sample menu.
- Documentation site (separate effort).

## Catalog file format

Authored by you and future contributors. Lives in `catalog/`, embedded via `embed.FS` at build time so the app is a single binary.

```kconfig
# catalog/peripherals/uart.kconfig

config UART
    bool "UART"
    depends on GPIO
    select BOOT_ROM_HELPERS
    help
      Polled and interrupt-driven UART routines for the
      RP2350 PL011 controller. Adds ~480 bytes of flash
      when enabled.

if UART

config UART_INSTANCE
    int "Instance"
    range 0 1
    default 0

config UART_BAUD
    int "Baud rate"
    default 115200

config UART_IRQ
    bool "IRQ handler"
    default y

endif
```

Each module declares its source files in a sidecar `module.toml`:

```toml
# catalog/peripherals/uart.module.toml
symbol = "UART"
sources = ["uart.s", "uart_irq.s", "uart_baud.s"]
sources_if_UART_IRQ = ["uart_irq.s"]   # conditional inclusion
```

Keep `.kconfig` files small and topical — one peripheral per file. The catalog should grow horizontally (more files) not vertically (giant monolithic files).

## Project file format

User-visible save format. TOML so power users can hand-edit and version-control it.

```toml
# blink-led.rpasm
name = "blink-led"
target = "rp2350-riscv"
rpasm_version = "1.0"

[features]
UART = true
UART_INSTANCE = 0
UART_BAUD = 115200
UART_IRQ = true
GPIO = true
DMA = true

[pins]
uart_tx = "GPIO0"
uart_rx = "GPIO1"

[user_source]
files = ["src/main.s"]
```

The `rpasm_version` field is the migration anchor. When you change the catalog schema, bump this and write a migration function that upgrades old project files.

## Build engine details

The naive approach is fine for v1: full rebuild every time, no caching. Assembly builds are fast (< 2 seconds for typical projects). Add incremental builds only when users complain.

Pseudocode for the build pipeline:

```
1. Load project file → ProjectState
2. Load catalog → SymbolTable
3. Resolve(ProjectState, SymbolTable) → ResolvedConfig
   - propagate select chains
   - evaluate depends on
   - fail fast on unsatisfied constraints
4. CollectSources(ResolvedConfig) → []SourceFile
5. GenerateLinkerScript(target, ResolvedConfig) → string
6. For each source file: Assemble(source) → object file
7. Link(objects, linker_script) → ELF
8. ConvertToUF2(ELF) → UF2 bytes
9. Write UF2 to output path
```

Every step returns errors with enough context that the GUI can highlight the offending feature or source line.

## Pingo integration

The killer cross-product feature. Two integration points:

1. **Pin assignment links**: in rp-asm Studio's detail panel, pin fields show an "open in Pingo" link. Clicking it launches Pingo (or focuses an existing window) with the current pin map pre-loaded and the relevant pin highlighted.
2. **Shared pin map format**: define a small TOML schema both apps read and write. Initially just a flat `pin_name → GPIO_N` table; grow as needed.

Implement via a local IPC mechanism — Unix domain socket on macOS/Linux, named pipe on Windows. Pingo and rp-asm Studio register on startup; if the other is already running, they negotiate the handoff.

## Testing strategy

- **Catalog parser**: golden tests on `.kconfig` snippets. ~50 cases covers every construct.
- **Resolver**: table-driven tests of `(initial state) → (resolved state)`. ~30 cases covers the constraint logic.
- **Build engine**: 5-10 end-to-end golden projects that produce known UF2 hashes. Run on real hardware in CI via a self-hosted runner with a Pico 2 attached (`picotool` + a serial loopback for verification).
- **GUI**: smoke tests only. Gio's testing story is weak; don't over-invest. Manual exercise via the sample projects.

## Distribution

Single statically-linked Go binary per platform. No installer for v1 — users download a zip, drag the app to Applications or run it directly.

- macOS: universal binary (arm64 + amd64), notarized, code-signed
- Windows: signed `.exe`, optional MSIX later
- Linux: tarball with the binary and a `.desktop` file; AppImage in v1.1

The whole thing should be < 20 MB before bundling the toolchain. With the toolchain (phase 2.0): ~150 MB.

## What to deliberately defer

Build features list in priority order, but do not build any of these in v1:

- Embedded code editor (open in external editor instead)
- AI-assisted feature selection (steal the pattern from Pingo once it's proven there)
- Multi-project workspaces (one project per window)
- Cloud sync of project files
- Plugin system for third-party catalog modules
- Localization (English only)

Each of these is a project of its own. v1's job is to prove the model works.

## Success criteria for v1

A user who has never seen Kconfig, Make, or a `arm-none-eabi-*` toolchain can:

1. Download and launch the app
2. Pick a target
3. Toggle four peripherals
4. Hit Build → Flash
5. See an LED blink on their Pico 2

Time from download to blinking LED: under 10 minutes. That's the bar. Everything else is decoration.
