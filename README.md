# ticktrace Studio

Visual configurator, build engine, and flashing tool for the [ticktrace Assembly SDK](https://github.com/ticktrace-sdk/rp-asm) — pure-assembly firmware for the Raspberry Pi RP2040 and RP2350.

## What it is

ticktrace Studio lets you configure, build, and flash assembly-language firmware without touching Make, CMake, or Kconfig directly. Two interfaces ship side by side:

| Tool | What it does |
|------|-------------|
| `rpasm` | CLI — build and flash from a terminal |
| `rpasm-studio` | GUI — visual catalog browser, build log, and one-click BOOTSEL flash |

## Requirements

- Go 1.24+
- `arm-none-eabi-as` and `arm-none-eabi-ld` on `$PATH`
- [ticktrace SDK](https://github.com/ticktrace-sdk/rp-asm) (included as a git submodule at `sdk/`)

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

## Repository layout

```
studio/          Go source — CLI, GUI, build engine, catalog parser
sdk/             ticktrace Assembly SDK (git submodule)
private/         Internal docs — not distributed
```

## License

AGPL-3.0-or-later. A commercial license is available from Amken LLC for use cases that cannot comply with the AGPL — see [`sdk/COMMERCIAL-LICENSE.md`](sdk/COMMERCIAL-LICENSE.md).
