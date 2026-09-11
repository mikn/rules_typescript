// Package toolpack writes a tools release asset: one gzipped tarball holding
// the four Go tools under a fixed prefix, byte-identical for the same inputs.
package toolpack

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"sort"
)

// Binaries are the entries of every asset.
var Binaries = []string{
	"tsaction", "ts_launcher", "lcov_merger", "copy_to_workspace",
}

// Prefix is the one directory an asset holds, and the asset's basename.
func Prefix(version, platform string) string {
	return fmt.Sprintf("rules_typescript-tools-%s-%s", version, platform)
}

// Pack writes the asset for version and platform to out from the binaries at
// paths, keyed by the names in Binaries, and returns its SRI integrity.
func Pack(out, version, platform string, paths map[string]string) (
	string, error,
) {
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	sort.Strings(names)
	if !equal(names, sorted(Binaries)) {
		return "", fmt.Errorf("toolpack: the asset holds %v, got %v", Binaries, names)
	}
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer f.Close()
	digest := sha256.New()
	gz, err := gzip.NewWriterLevel(io.MultiWriter(f, digest), gzip.BestCompression)
	if err != nil {
		return "", err
	}
	archive := tar.NewWriter(gz)
	prefix := Prefix(version, platform)
	if err := archive.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     prefix + "/",
		Mode:     0o755,
		Format:   tar.FormatPAX,
	}); err != nil {
		return "", err
	}
	for _, name := range names {
		if err := addFile(archive, prefix+"/"+name, paths[name]); err != nil {
			return "", err
		}
	}
	if err := archive.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return "sha256-" + base64.StdEncoding.EncodeToString(digest.Sum(nil)), nil
}

func addFile(archive *tar.Writer, name, path string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := archive.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     0o755,
		Size:     info.Size(),
		Format:   tar.FormatPAX,
	}); err != nil {
		return err
	}
	_, err = io.Copy(archive, in)
	return err
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
