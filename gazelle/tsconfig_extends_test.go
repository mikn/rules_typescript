package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func writeTsConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A workspace member whose tsconfig only extends a shared base still has to
// contribute the base's aliases, and the base wrote them relative to itself.
func TestLoadTsConfigPaths_ExtendsBase(t *testing.T) {
	repo := t.TempDir()
	writeTsConfig(t, filepath.Join(repo, "packages/tsconfig-base/tsconfig.json"), `{
  "compilerOptions": {
    "paths": {"@/*": ["src/*"]}
  }
}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{"extends": "../../packages/tsconfig-base/tsconfig.json"}`)

	got := loadTsConfigPaths(leaf, "apps/web")
	want := map[string]string{"@/": "packages/tsconfig-base/src/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

// tsc replaces an inherited compilerOptions key wholesale, so a leaf that
// redeclares paths drops the base's other entries rather than merging them.
func TestLoadTsConfigPaths_ExtendsLeafReplacesWholeKey(t *testing.T) {
	repo := t.TempDir()
	writeTsConfig(t, filepath.Join(repo, "packages/tsconfig-base/tsconfig.json"), `{
  "compilerOptions": {
    "paths": {"@/*": ["src/*"], "@lib/*": ["lib/*"]}
  }
}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{
  "extends": "../../packages/tsconfig-base/tsconfig.json",
  "compilerOptions": {
    "paths": {"@/*": ["app/*"]}
  }
}`)

	got := loadTsConfigPaths(leaf, "apps/web")
	want := map[string]string{"@/": "apps/web/app/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

func TestLoadTsConfigPaths_ExtendsArrayLastWins(t *testing.T) {
	repo := t.TempDir()
	writeTsConfig(t, filepath.Join(repo, "apps/web/a.json"), `{
  "compilerOptions": {
    "paths": {"@/*": ["a/*"], "@only-a/*": ["only/*"]}
  }
}`)
	writeTsConfig(t, filepath.Join(repo, "apps/web/b.json"), `{
  "compilerOptions": {
    "paths": {"@/*": ["b/*"]}
  }
}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{"extends": ["./a", "./b.json"]}`)

	got := loadTsConfigPaths(leaf, "apps/web")
	want := map[string]string{"@/": "apps/web/b/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

// baseUrl and paths can come from different files in the chain: baseUrl is
// relative to the config that wrote it, and paths are relative to baseUrl.
func TestLoadTsConfigPaths_ExtendsInheritedBaseURL(t *testing.T) {
	repo := t.TempDir()
	writeTsConfig(t, filepath.Join(repo, "packages/tsconfig-base/tsconfig.json"), `{
  "compilerOptions": {"baseUrl": "src"}
}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{
  "extends": "../../packages/tsconfig-base/tsconfig.json",
  "compilerOptions": {
    "paths": {"@/*": ["./*"]}
  }
}`)

	got := loadTsConfigPaths(leaf, "apps/web")
	want := map[string]string{"@/": "packages/tsconfig-base/src/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

// A bare or scoped specifier resolves through node_modules, which a Bazel
// checkout does not have; the rest of the config still has to load.
func TestLoadTsConfigPaths_ExtendsBareSpecifierSkipped(t *testing.T) {
	repo := t.TempDir()
	leaf := filepath.Join(repo, "tsconfig.json")
	writeTsConfig(t, leaf, `{
  "extends": "@tsconfig/node20/tsconfig.json",
  "compilerOptions": {
    "paths": {"@/*": ["src/*"]}
  }
}`)

	got := loadTsConfigPaths(leaf, "")
	want := map[string]string{"@/": "src/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

func TestLoadTsConfigPaths_ExtendsCycleTerminates(t *testing.T) {
	repo := t.TempDir()
	leaf := filepath.Join(repo, "tsconfig.json")
	writeTsConfig(t, leaf, `{"extends": "./ring.json"}`)
	writeTsConfig(t, filepath.Join(repo, "ring.json"), `{
  "extends": "./tsconfig.json",
  "compilerOptions": {
    "paths": {"@/*": ["src/*"]}
  }
}`)

	done := make(chan map[string]string, 1)
	go func() { done <- loadTsConfigPaths(leaf, "") }()
	select {
	case got := <-done:
		want := map[string]string{"@/": "src/"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("loadTsConfigPaths did not terminate on an extends cycle")
	}
}

// A base outside the repository has no label to name it, and a "../" prefix in
// path_aliases is worse than no alias at all.
func TestLoadTsConfigPaths_ExtendsOutsideTheRepoIsDropped(t *testing.T) {
	root := t.TempDir()
	writeTsConfig(t, filepath.Join(root, "shared/tsconfig.json"), `{
  "compilerOptions": {
    "paths": {"@/*": ["src/*"]}
  }
}`)
	leaf := filepath.Join(root, "repo/apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{"extends": "../../../shared/tsconfig.json"}`)

	if got := loadTsConfigPaths(leaf, "apps/web"); got != nil {
		t.Errorf("loadTsConfigPaths: got %v, want no aliases", got)
	}
}

// Two branches of an extends array reaching the same base is not a cycle: the
// base is read again, because where it lands in the merge order is what
// decides whether it beats a config declared between the two branches.
func TestLoadTsConfigPaths_ExtendsDiamondReReadsTheSharedBase(t *testing.T) {
	repo := t.TempDir()
	writeTsConfig(t, filepath.Join(repo, "apps/web/shared.json"), `{
  "compilerOptions": {"paths": {"@/*": ["shared/*"]}}
}`)
	writeTsConfig(t, filepath.Join(repo, "apps/web/a.json"), `{"extends": "./shared.json"}`)
	writeTsConfig(t, filepath.Join(repo, "apps/web/b.json"), `{"extends": "./shared.json"}`)
	writeTsConfig(t, filepath.Join(repo, "apps/web/middle.json"), `{
  "compilerOptions": {"paths": {"@/*": ["middle/*"]}}
}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	writeTsConfig(t, leaf, `{"extends": ["./a.json", "./middle.json", "./b.json"]}`)

	got := loadTsConfigPaths(leaf, "apps/web")
	want := map[string]string{"@/": "apps/web/shared/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}
