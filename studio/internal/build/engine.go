package build

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/amken3d/rp-asm/studio/internal/project"
)

type Options struct {
	Resolved  *project.Resolved
	Root      string
	OutDir    string
	Toolchain *Toolchain
	Verbose   bool
	// Stdout/Stderr override where toolchain output goes. If nil, falls back
	// to os.Stdout / os.Stderr (CLI default). GUI callers pass a writer that
	// funnels into their log pane so assembler errors surface in-app instead
	// of disappearing into the terminal.
	Stdout io.Writer
	Stderr io.Writer
	// Slot selects per-slot build mode for bootloader projects. "" (default)
	// produces the full SSBL+TSBL+app firmware UF2. "a" or "b" links the app
	// at that slot's base and packs a slot-only UF2 (app + footer at the
	// chosen slot addresses) for over-the-air style A/B updates that don't
	// re-flash the bootloader. Only valid when Resolved.Project.Bootloader
	// is set.
	Slot string
}

type Result struct {
	Objects []string
	Elf     string
	Bin     string
	Uf2     string
	// Memory is best-effort. Nil or partial if ld didn't print memory usage or
	// `size` wasn't available; build still succeeded.
	Memory *MemoryUsage
	// Bootloader is populated only for projects with [bootloader] set —
	// breaks the firmware UF2 down by stage.
	Bootloader *BootloaderUsage
}

func Build(opts *Options) (*Result, error) {
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return nil, err
	}
	tgt := opts.Resolved.Target
	// Toolchain CWD: parent of the studio module root. This is the SDK root
	// containing src/, include/, link/, examples/ — matching how the Makefile
	// is invoked. Relative `.include "src/nvic.S"` in .S files resolves here.
	workDir := filepath.Dir(opts.Root)

	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	asArgs := append([]string{}, tgt.AsFlags...)
	for _, inc := range tgt.AsIncludes {
		asArgs = append(asArgs, "-I", resolve(opts.Root, inc))
	}
	// Also expose the SDK root as an -I path so `.include "src/foo.S"` works
	// even if the CWD differs (defence in depth — CWD is the primary mechanism).
	asArgs = append(asArgs, "-I", workDir)

	objs := make([]string, 0, len(opts.Resolved.Sources))
	for _, src := range opts.Resolved.Sources {
		srcPath := resolve(opts.Root, src)
		objName := objectName(src) + ".o"
		objPath := filepath.Join(opts.OutDir, objName)
		args := append([]string{}, asArgs...)
		args = append(args, "-o", objPath, srcPath)
		if err := runCmd(opts.Toolchain.As, args, workDir, stdout, stderr, opts.Verbose); err != nil {
			return nil, fmt.Errorf("as %s: %w", src, err)
		}
		objs = append(objs, objPath)
	}

	if opts.Slot != "" && opts.Resolved.Project.Bootloader == nil {
		return nil, fmt.Errorf("--slot requires the project to have [bootloader] set")
	}
	var ldScript string
	switch {
	case opts.Slot == "b":
		ldScript = resolve(opts.Root, ldScriptSlotB)
	case opts.Resolved.Project.Bootloader != nil:
		// Slot "" or "a": app links at slot A's base.
		ldScript = resolve(opts.Root, ldScriptSlotA)
	case opts.Resolved.Project.Layout == "flash":
		ldScript = resolve(opts.Root, tgt.LdScriptFlash)
	case opts.Resolved.Project.Layout == "sram":
		ldScript = resolve(opts.Root, tgt.LdScriptSram)
	default:
		return nil, fmt.Errorf("unsupported layout %q", opts.Resolved.Project.Layout)
	}

	elf := filepath.Join(opts.OutDir, opts.Resolved.Project.Name+".elf")
	mapFile := filepath.Join(opts.OutDir, opts.Resolved.Project.Name+".map")
	ldArgs := []string{"-T", ldScript, "--print-memory-usage"}
	ldArgs = append(ldArgs, tgt.LdFlags...)
	ldArgs = append(ldArgs, "-Map="+mapFile, "-o", elf)
	ldArgs = append(ldArgs, objs...)
	// ld writes --print-memory-usage to stdout. Tee it into a capture buffer
	// so we can parse the region table after the link succeeds, while still
	// forwarding to the user's stdout writer (build-log pane / terminal).
	ldCapture := &bytes.Buffer{}
	if err := runCmd(opts.Toolchain.Ld, ldArgs, workDir, io.MultiWriter(stdout, ldCapture), stderr, opts.Verbose); err != nil {
		return nil, fmt.Errorf("ld: %w", err)
	}

	bin := filepath.Join(opts.OutDir, opts.Resolved.Project.Name+".bin")
	if err := runCmd(opts.Toolchain.Objcopy, []string{"-O", "binary", elf, bin}, workDir, stdout, stderr, opts.Verbose); err != nil {
		return nil, fmt.Errorf("objcopy: %w", err)
	}

	var (
		uf2       string
		blUsage   *BootloaderUsage
	)
	if opts.Resolved.Project.Bootloader != nil {
		appBin, err := os.ReadFile(bin)
		if err != nil {
			return nil, fmt.Errorf("read app bin: %w", err)
		}
		if opts.Slot != "" {
			uf2, blUsage, err = buildSlotOnlyUF2(opts, appBin)
		} else {
			uf2, blUsage, err = buildBootloaderChain(opts, appBin)
		}
		if err != nil {
			return nil, fmt.Errorf("bootloader chain: %w", err)
		}
	} else {
		var loadAddr uint32
		switch opts.Resolved.Project.Layout {
		case "flash":
			loadAddr = tgt.FlashLoadAddr
		case "sram":
			loadAddr = tgt.SramLoadAddr
		}
		uf2 = filepath.Join(opts.OutDir, opts.Resolved.Project.Name+".uf2")
		if err := PackUF2(bin, uf2, loadAddr, tgt.Uf2FamilyID); err != nil {
			return nil, fmt.Errorf("uf2: %w", err)
		}
	}

	mem := &MemoryUsage{
		Regions:  parseMemoryRegions(ldCapture.String()),
		Sections: runSize(opts.Toolchain.Size, elf, workDir),
	}
	return &Result{Objects: objs, Elf: elf, Bin: bin, Uf2: uf2, Memory: mem, Bootloader: blUsage}, nil
}

func resolve(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Clean(filepath.Join(root, p))
}

func objectName(src string) string {
	base := filepath.Base(src)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func runCmd(bin string, args []string, dir string, stdout, stderr io.Writer, verbose bool) error {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	if verbose {
		fmt.Fprintf(stderr, "(cd %s && %s %s)\n", dir, bin, strings.Join(args, " "))
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
