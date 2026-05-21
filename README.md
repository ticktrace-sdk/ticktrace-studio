# ticktrace Studio

Visual configurator, build engine, and flashing tool for the [ticktrace Assembly SDK](https://github.com/ticktrace-sdk/rp-asm) — pure-assembly firmware for the Raspberry Pi RP2040 and RP2350.

## What it is

ticktrace Studio lets you configure, build, and flash assembly-language firmware without touching Make, CMake, or Kconfig directly. Two interfaces ship side by side:

| Tool | What it does |
|------|-------------|
| `rpasm` | CLI — build and flash from a terminal |
| `rpasm-studio` | GUI — visual catalog browser, build log, and one-click BOOTSEL flash |

## Download

Pre-built binaries for macOS (Intel + Apple Silicon), Windows, and Linux are on the [Releases page](https://github.com/ticktrace-sdk/ticktrace-studio/releases). Each archive contains both `rpasm-studio` (GUI) and `rpasm` (CLI).

On first launch Studio checks for an ARM toolchain (`arm-none-eabi-as`/`-ld`/`-objcopy`) and offers to download a managed copy (~150 MB) into `~/.ticktrace/toolchain/` if none is found. From the CLI:

```sh
rpasm install-toolchain
```

If you already have a toolchain via Homebrew, scoop, or ARM's official installer, Studio picks it up automatically — no download needed.

## Build from source

- Go 1.26+ (matches `studio/go.mod`)
- [ticktrace SDK](https://github.com/ticktrace-sdk/rp-asm) (included as a git submodule at `sdk/`)
- An ARM toolchain — either system-installed, or installed via `rpasm install-toolchain` (above)

### GUI dependencies (rpasm-studio only)

`rpasm-studio` uses [ImmyGo](https://immygo.app), which builds on [Gio](https://gioui.org) and requires native graphics libraries. The CLI (`rpasm`) has no extra requirements.

| Platform | Command |
|----------|---------|
| Linux (Debian/Ubuntu) | `sudo apt install libwayland-dev libxkbcommon-x11-dev libgles2-mesa-dev libegl1-mesa-dev libx11-xcb-dev libvulkan-dev` |
| macOS | `xcode-select --install` |
| Windows | No additional dependencies |

## Getting started

```sh
git clone --recurse-submodules https://github.com/ticktrace-sdk/ticktrace-studio
cd ticktrace-studio

# Build the CLI
go build ./studio/cmd/rpasm

# Build the GUI
go build ./studio/cmd/rpasm-studio
```

The SDK submodule path is auto-detected. Override it with:

```sh
RPASM_SDK=/path/to/sdk rpasm build
```

## Cutting a release

1. Tag the commit you want to ship: `git tag v0.1.0 && git push --tags`.
2. The `release` workflow builds Studio natively on macOS (arm64 + amd64), Windows, and Linux, then publishes the archives + `checksums.txt` to a new GitHub release.
3. Verify the SHA-256 in the release matches `dist/checksums.txt`.

To smoke-test the release pipeline without publishing, trigger the workflow manually via the **Actions → release → Run workflow** button — it produces the same archives as build artifacts but skips the `gh release create` step.

## Repository layout

```
studio/          Go source — CLI, GUI, build engine, catalog parser
sdk/             ticktrace Assembly SDK (git submodule)
private/         Internal docs — not distributed
```

## License

AGPL-3.0-or-later. A commercial license is available from Amken LLC for use cases that cannot comply with the AGPL — see [`sdk/COMMERCIAL-LICENSE.md`](sdk/COMMERCIAL-LICENSE.md).
