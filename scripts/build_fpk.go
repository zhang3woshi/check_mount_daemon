package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"
)

const appName = "zhang3woshi.checkmountdaemon"

type manifestData struct {
	Version  string
	Platform string
}

func main() {
	var (
		version  = flag.String("version", "", "application version")
		platform = flag.String("platform", "", "fnOS platform, e.g. x86 or arm")
		binary   = flag.String("binary", "", "path to the linux binary to package")
		fnpack   = flag.String("fnpack", "fnpack", "path to fnpack executable")
		stage    = flag.String("stage", "", "temporary package directory")
		output   = flag.String("output", "", "output fpk path")
	)
	flag.Parse()

	if *version == "" || *platform == "" || *binary == "" || *stage == "" || *output == "" {
		exitErr(errors.New("version, platform, binary, stage and output are required"))
	}
	if err := validateBinaryPlatform(*binary, *platform); err != nil {
		exitErr(err)
	}

	root, err := os.Getwd()
	if err != nil {
		exitErr(err)
	}

	templateDir := filepath.Join(root, "packaging", "fnos", "template")
	defaultConfig := filepath.Join(root, "packaging", "config", "config.json")

	if err := os.RemoveAll(*stage); err != nil {
		exitErr(err)
	}
	if err := copyDir(templateDir, *stage); err != nil {
		exitErr(err)
	}
	if err := renderManifest(filepath.Join(templateDir, "manifest.tmpl"), filepath.Join(*stage, "manifest"), manifestData{
		Version:  *version,
		Platform: *platform,
	}); err != nil {
		exitErr(err)
	}

	if err := os.MkdirAll(filepath.Join(*stage, "app", "bin"), 0o755); err != nil {
		exitErr(err)
	}
	if err := os.MkdirAll(filepath.Join(*stage, "app", "defaults"), 0o755); err != nil {
		exitErr(err)
	}
	uiImages := filepath.Join(*stage, "app", "ui", "images")
	if err := os.MkdirAll(uiImages, 0o755); err != nil {
		exitErr(err)
	}

	if err := copyFile(*binary, filepath.Join(*stage, "app", "bin", "check_mount_daemon"), 0o755); err != nil {
		exitErr(err)
	}
	if err := copyFile(defaultConfig, filepath.Join(*stage, "app", "defaults", "config.json"), 0o644); err != nil {
		exitErr(err)
	}
	if err := copyFile(filepath.Join(templateDir, "ICON.PNG"), filepath.Join(uiImages, "icon_64.png"), 0o644); err != nil {
		exitErr(err)
	}
	if err := copyFile(filepath.Join(templateDir, "ICON_256.PNG"), filepath.Join(uiImages, "icon_256.png"), 0o644); err != nil {
		exitErr(err)
	}

	if err := runFnpack(*fnpack, *stage); err != nil {
		exitErr(err)
	}

	generated, err := findGeneratedFPK(root, *stage)
	if err != nil {
		exitErr(err)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		exitErr(err)
	}
	if err := os.RemoveAll(*output); err != nil {
		exitErr(err)
	}
	if err := os.Rename(generated, *output); err != nil {
		exitErr(err)
	}

	fmt.Println(*output)
}

// validateBinaryPlatform prevents an amd64 executable from being accidentally
// packaged as an ARM application (and vice versa). fnOS reports these packages
// as installed but the service cannot start, so fail during the build instead.
func validateBinaryPlatform(path, platform string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open binary: %w", err)
	}
	defer file.Close()

	header := make([]byte, 20)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("read binary header: %w", err)
	}
	if string(header[:4]) != "\x7fELF" {
		return fmt.Errorf("binary %s is not a Linux ELF executable", path)
	}
	if header[4] != 2 {
		return fmt.Errorf("binary %s is not a 64-bit ELF executable", path)
	}

	machine := binary.LittleEndian.Uint16(header[18:20])
	var expected uint16
	switch platform {
	case "x86":
		expected = 62 // EM_X86_64
	case "arm":
		expected = 183 // EM_AARCH64
	default:
		return fmt.Errorf("unsupported fnOS platform %q; expected x86 or arm", platform)
	}
	if machine != expected {
		return fmt.Errorf("binary architecture %d does not match fnOS platform %s", machine, platform)
	}
	return nil
}

func findGeneratedFPK(root, stage string) (string, error) {
	candidates := []string{
		filepath.Join(stage, appName+".fpk"),
		filepath.Join(root, appName+".fpk"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("fnpack output not found in %s or %s", stage, root)
}

func renderManifest(src, dst string, data manifestData) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tpl, err := template.New("manifest").Parse(string(content))
	if err != nil {
		return err
	}
	file, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer file.Close()

	return tpl.Execute(file, data)
}

func runFnpack(fnpackPath, stage string) error {
	cmd := exec.Command(fnpackPath, "build", "-d", stage)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." || rel == "manifest.tmpl" {
			return nil
		}

		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	if mode == 0 {
		mode = 0o644
	}
	return os.Chmod(dst, normalizeMode(mode))
}

func normalizeMode(mode fs.FileMode) fs.FileMode {
	if runtime.GOOS == "windows" {
		if mode&0o111 != 0 {
			return 0o755
		}
		return 0o644
	}
	return mode.Perm()
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
