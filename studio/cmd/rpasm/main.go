package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/amken3d/rp-asm/studio/internal/build"
	"github.com/amken3d/rp-asm/studio/internal/catalog"
	"github.com/amken3d/rp-asm/studio/internal/flash"
	"github.com/amken3d/rp-asm/studio/internal/project"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "validate":
		os.Exit(cmdValidate(os.Args[2:]))
	case "build":
		os.Exit(cmdBuild(os.Args[2:]))
	case "flash":
		os.Exit(cmdFlash(os.Args[2:]))
	case "reboot":
		os.Exit(cmdReboot(os.Args[2:]))
	case "info":
		os.Exit(cmdInfo(os.Args[2:]))
	case "bootinfo":
		os.Exit(cmdBootInfo(os.Args[2:]))
	case "doctor":
		os.Exit(cmdDoctor(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `rpasm - rp-asm Studio CLI

usage:
  rpasm validate [--root DIR] <project.toml>
  rpasm build    [--root DIR] [--out DIR] [-v] <project.toml>
  rpasm flash    [--method rpasmboot|drive] [--slot a|b] (--uf2 <path> | [--root DIR] <project.toml>)
  rpasm reboot   [--bootsel]
  rpasm info
  rpasm bootinfo
  rpasm doctor   [--root DIR]

Paths in project and catalog TOML are resolved relative to --root (default: studio module root, auto-detected from CWD).
`)
}

func defaultRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	for d := cwd; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(d, "catalog")); err == nil {
				return d
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return cwd
		}
		d = parent
	}
}

func loadResolved(root, projPath string) (*project.Resolved, error) {
	cat, err := catalog.Load(filepath.Join(root, "catalog"))
	if err != nil {
		return nil, fmt.Errorf("loading catalog: %w", err)
	}
	if !filepath.IsAbs(projPath) {
		cwd, _ := os.Getwd()
		projPath = filepath.Join(cwd, projPath)
	}
	proj, err := project.Load(projPath)
	if err != nil {
		return nil, fmt.Errorf("loading project: %w", err)
	}
	res, err := project.Resolve(proj, cat)
	if err != nil {
		return nil, fmt.Errorf("resolving project: %w", err)
	}
	return res, nil
}

func cmdValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	root := fs.String("root", defaultRoot(), "studio module root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "validate: expected exactly one project file")
		return 2
	}
	res, err := loadResolved(*root, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("project:  %s\n", res.Project.Name)
	fmt.Printf("target:   %s (%s)\n", res.Target.Name, res.Target.Arch)
	fmt.Printf("layout:   %s\n", res.Project.Layout)
	fmt.Printf("modules:  %d enabled\n", len(res.Modules))
	for _, m := range res.Modules {
		fmt.Printf("  [%3d] %-14s  %s\n", m.Order, m.Symbol, m.Name)
	}
	fmt.Printf("sources:  %d total\n", len(res.Sources))
	for _, s := range res.Sources {
		fmt.Printf("  %s\n", s)
	}
	return 0
}

func cmdBuild(args []string) int {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	root := fs.String("root", defaultRoot(), "studio module root")
	out := fs.String("out", "", "build output directory (default: <root>/build/<project>)")
	verbose := fs.Bool("v", false, "echo tool invocations")
	slot := fs.String("slot", "", "for [bootloader] projects: build a slot-only UF2 (a|b). Default: full chain.")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "build: expected exactly one project file")
		return 2
	}
	if *slot != "" && *slot != "a" && *slot != "b" {
		fmt.Fprintf(os.Stderr, "--slot must be \"a\" or \"b\", got %q\n", *slot)
		return 2
	}
	res, err := loadResolved(*root, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	tc, err := build.Detect(res.Target.ToolchainPrefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	outDir := *out
	if outDir == "" {
		outDir = filepath.Join(*root, "build", res.Project.Name)
	}
	result, err := build.Build(&build.Options{
		Resolved:  res,
		Root:      *root,
		OutDir:    outDir,
		Toolchain: tc,
		Verbose:   *verbose,
		Slot:      *slot,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("  ELF  %s\n", result.Elf)
	fmt.Printf("  BIN  %s\n", result.Bin)
	fmt.Printf("  UF2  %s\n", result.Uf2)
	if result.Memory != nil {
		for _, r := range result.Memory.Regions {
			fmt.Printf("  %-8s %9d / %9d B  (%.2f%%)\n", r.Name+":", r.Used, r.Size, r.Percent())
		}
	}
	if result.Bootloader != nil {
		for _, s := range result.Bootloader.Stages {
			fmt.Printf("  %-8s %9d / %9d B  (%.2f%%)  @ 0x%08x\n",
				s.Name+":", s.Used, s.Capacity, s.Percent(), s.Base)
		}
	}
	return 0
}

func cmdFlash(args []string) int {
	fs := flag.NewFlagSet("flash", flag.ExitOnError)
	root := fs.String("root", defaultRoot(), "studio module root")
	method := fs.String("method", "auto", "flash method: auto | rpasmboot | drive")
	uf2Path := fs.String("uf2", "", "flash this UF2 directly (skips project resolution)")
	slot := fs.String("slot", "", "for [bootloader] projects: push only this slot (a|b). Triggers a rebuild and flashes the slot-only UF2.")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *slot != "" && *slot != "a" && *slot != "b" {
		fmt.Fprintf(os.Stderr, "--slot must be \"a\" or \"b\", got %q\n", *slot)
		return 2
	}
	if *slot != "" && *uf2Path != "" {
		fmt.Fprintln(os.Stderr, "flash: --slot and --uf2 are mutually exclusive")
		return 2
	}

	var uf2 string
	switch {
	case *uf2Path != "":
		if fs.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "flash: --uf2 and a project file are mutually exclusive")
			return 2
		}
		uf2 = *uf2Path
	case fs.NArg() == 1:
		res, err := loadResolved(*root, fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if *slot != "" {
			// Per-slot mode: rebuild for the chosen slot, flash the slot-only UF2.
			tc, err := build.Detect(res.Target.ToolchainPrefix)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			outDir := filepath.Join(*root, "build", res.Project.Name)
			result, err := build.Build(&build.Options{
				Resolved:  res,
				Root:      *root,
				OutDir:    outDir,
				Toolchain: tc,
				Slot:      *slot,
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			uf2 = result.Uf2
			fmt.Fprintf(os.Stderr, "built slot-%s UF2: %s\n", *slot, uf2)
		} else {
			// Standard path: flash whatever the project's last build produced.
			uf2Name := res.Project.Name + ".uf2"
			if res.Project.Bootloader != nil {
				uf2Name = "firmware_" + res.Project.Name + ".uf2"
			}
			uf2 = filepath.Join(*root, "build", res.Project.Name, uf2Name)
			if _, err := os.Stat(uf2); err != nil {
				fmt.Fprintf(os.Stderr, "uf2 not built yet: %s\n(run `rpasm build %s` first)\n", uf2, fs.Arg(0))
				return 1
			}
		}
	default:
		fmt.Fprintln(os.Stderr, "flash: expected one project file or --uf2 <path>")
		return 2
	}

	var prefer flash.Method
	switch *method {
	case "auto", "":
		prefer = ""
	case "rpasmboot":
		prefer = flash.MethodRpasmboot
	case "drive":
		prefer = flash.MethodDrive
	default:
		fmt.Fprintf(os.Stderr, "unknown method %q (auto | rpasmboot | drive)\n", *method)
		return 2
	}

	result, err := flash.Flash(&flash.Options{
		Uf2Path: uf2,
		Prefer:  prefer,
		Log:     func(s string) { fmt.Fprintln(os.Stderr, s) },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("flashed via %s (%s)\n", result.Method, result.Target)
	return 0
}

func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	root := fs.String("root", defaultRoot(), "studio module root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cat, err := catalog.Load(filepath.Join(*root, "catalog"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("root:    %s\n", *root)
	fmt.Printf("modules: %d\n", len(cat.Modules))
	fmt.Printf("targets: %d\n", len(cat.Targets))
	fmt.Println()
	rc := 0
	for name, t := range cat.Targets {
		fmt.Printf("[target %s]\n", name)
		tc, err := build.Detect(t.ToolchainPrefix)
		if err != nil {
			fmt.Printf("  ERROR: %s\n", err)
			rc = 1
			continue
		}
		fmt.Printf("  as:      %s\n           %s\n", tc.As, tc.Version(tc.As))
		fmt.Printf("  ld:      %s\n           %s\n", tc.Ld, tc.Version(tc.Ld))
		fmt.Printf("  objcopy: %s\n           %s\n", tc.Objcopy, tc.Version(tc.Objcopy))
	}
	fmt.Println()
	if err := checkBoard(); err != nil {
		rc = 1
	}
	return rc
}
