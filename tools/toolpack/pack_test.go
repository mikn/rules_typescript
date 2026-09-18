package toolpack

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T, dir string) map[string]string {
	paths := map[string]string{}
	for _, name := range Binaries {
		path := filepath.Join(dir, name)
		script := []byte("#!/bin/sh\necho " + name + "\n")
		if err := os.WriteFile(path, script, 0o600); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	return paths
}

func entries(t *testing.T, path string) map[string]*tar.Header {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if !gz.ModTime.IsZero() || gz.Name != "" {
		t.Errorf("gzip header carries mtime %v name %q", gz.ModTime, gz.Name)
	}
	out := map[string]*tar.Header{}
	archive := tar.NewReader(gz)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[header.Name] = header
	}
}

func TestPackIsDeterministicAndLaidOutAsTheRelease(t *testing.T) {
	dir := t.TempDir()
	paths := fixture(t, dir)
	a := filepath.Join(dir, "a.tar.gz")
	b := filepath.Join(dir, "b.tar.gz")
	sriA, err := Pack(a, "1", "linux_amd64", paths)
	if err != nil {
		t.Fatal(err)
	}
	then := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(paths["tsaction"], then, then); err != nil {
		t.Fatal(err)
	}
	sriB, err := Pack(b, "1", "linux_amd64", paths)
	if err != nil {
		t.Fatal(err)
	}
	if sriA != sriB {
		t.Errorf("two packs of the same files differ: %s vs %s", sriA, sriB)
	}
	bytesA, _ := os.ReadFile(a)
	bytesB, _ := os.ReadFile(b)
	if string(bytesA) != string(bytesB) {
		t.Error("two packs of the same files differ byte for byte")
	}
	if len(sriA) != len("sha256-")+44 {
		t.Errorf("SRI %q is not sha256-<base64>", sriA)
	}

	got := entries(t, a)
	prefix := Prefix("1", "linux_amd64")
	if prefix != "rules_typescript-tools-1-linux_amd64" {
		t.Errorf("prefix = %q", prefix)
	}
	if _, ok := got[prefix+"/"]; !ok {
		t.Errorf("no directory entry %s/", prefix)
	}
	for _, name := range Binaries {
		header, ok := got[prefix+"/"+name]
		if !ok {
			t.Errorf("no entry %s/%s in %v", prefix, name, keys(got))
			continue
		}
		if header.Mode != 0o755 || header.Uid != 0 || header.Gid != 0 ||
			header.ModTime.Unix() != 0 {
			t.Errorf("%s: mode %o uid %d gid %d mtime %v", name, header.Mode,
				header.Uid, header.Gid, header.ModTime)
		}
	}
	if len(got) != len(Binaries)+1 {
		t.Errorf("the asset holds %d entries, want %d: %v", len(got),
			len(Binaries)+1, keys(got))
	}
}

func TestPackRefusesAnotherSetOfBinaries(t *testing.T) {
	dir := t.TempDir()
	paths := fixture(t, dir)
	delete(paths, "lcov_merger")
	out := filepath.Join(dir, "x.tar.gz")
	if _, err := Pack(out, "1", "linux_amd64", paths); err == nil {
		t.Error("a set without lcov_merger was packed")
	}
}

func keys(m map[string]*tar.Header) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
