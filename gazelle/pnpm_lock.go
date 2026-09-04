package typescript

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const pnpmLockfileName = "pnpm-lock.yaml"

// loadNpmInventory reads the workspace-root pnpm-lock.yaml into the npm
// inventory, the wider set of names it mentions, and the pnpm importer dirs.
//
// nil and empty are different claims. nil is "no information" -- there is no
// lockfile, it could not be read, or its format version this reader does not
// handle -- and every caller that gates on the inventory keeps its heuristics
// in that case. A non-nil map is the lockfile's own answer, and an empty one
// says the workspace declares nothing.
func loadNpmInventory(repoRoot string) (inventory map[string]string, lockNames map[string]bool, members map[string]bool) {
	path := filepath.Join(repoRoot, pnpmLockfileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil
	}
	inventory, err = parsePnpmLockInventory(string(data))
	if err != nil {
		log.Printf("typescript: %s: %v\n"+
			"The npm inventory is unavailable, so codegen "+
			"targets fall back to file-presence heuristics and a `node:` import "+
			"gets no @types/node dep.", path, err)
		return nil, nil, nil
	}
	lines := strings.Split(string(data), "\n")
	return inventory, parsePnpmLockNames(lines), parsePnpmImporterDirs(lines)
}

// parsePnpmImporterDirs returns the workspace-relative directories the
// `importers:` section lists, which is the pnpm workspace's own membership --
// the one place that tells a member's package name from an installed one.
// The root importer is spelled "." and answers as "".
func parsePnpmImporterDirs(lines []string) map[string]bool {
	body, ok := pnpmSection(lines, "importers")
	if !ok {
		return nil
	}
	dirs := make(map[string]bool)
	for _, raw := range body {
		indent, stripped, ok := pnpmContentLine(raw)
		if !ok || indent != 2 || !strings.HasSuffix(stripped, ":") {
			continue
		}
		dir := strings.Trim(strings.TrimSuffix(stripped, ":"), "'\"")
		if dir == "." {
			dir = ""
		}
		if dir == "" || !strings.HasPrefix(dir, "..") {
			dirs[dir] = true
		}
	}
	return dirs
}

// pnpmSupportedLockfileMajors are the lockfile format majors this reader
// handles, kept to the pair npm/private/npm_translate_lock.bzl reads so the
// inventory cannot claim a namespace the hub was never built from.
var pnpmSupportedLockfileMajors = map[string]bool{"6": true, "9": true}

// parsePnpmLockInventory returns the npm package names the hub declares a flat
// //:<label> target for, mapped to "" -- the inventory carries no label of its
// own, so a ts_npm_hub directive still chooses the repository.
//
// npm/lazy.bzl builds that namespace from three sources: every `snapshots:`
// entry whose `packages:` entry exists and is buildable on a platform the
// ruleset can name, every workspace `link:`, and every npm alias an importer
// declares. This reader mirrors all three, and under-claims wherever it cannot
// be exact: a name missing from the inventory only costs a dep Gazelle could
// have written, while a name the hub does not declare fails analysis for the
// whole workspace.
//
// The one deliberate under-claim is platform filtering. A package carrying
// `os:`/`cpu:`/`libc:` is dropped rather than matched against
// platforms/platforms.bzl, which would mean a second copy of that table in Go;
// what it costs is the native sidecars (@esbuild/linux-x64, fsevents), which
// no TypeScript source imports by name.
func parsePnpmLockInventory(content string) (map[string]string, error) {
	lines := strings.Split(content, "\n")

	version := pnpmLockfileVersion(lines)
	if version == "" {
		return nil, fmt.Errorf("no lockfileVersion: key, so this is not a pnpm lockfile")
	}
	major, _, _ := strings.Cut(version, ".")
	if !pnpmSupportedLockfileMajors[major] {
		return nil, fmt.Errorf("lockfileVersion %q is not supported; this reader "+
			"handles 6.x and 9.x, the same two npm/private/npm_translate_lock.bzl "+
			"reads. Did you mean to run the pnpm this ruleset ships "+
			"(`bazel run //:pnpm -- install --lockfile-only`)?", version)
	}

	packages := parsePnpmPackages(lines)
	snapshots, haveSnapshots := parsePnpmSnapshotPackageIDs(lines)
	links, aliases := parsePnpmImporterNames(lines)

	inventory := make(map[string]string, len(packages)+len(links)+len(aliases))
	declared := func(id string) bool {
		pkg, ok := packages[id]
		return ok && !pkg.platformRestricted
	}
	for id, pkg := range packages {
		// A v6 lockfile has no `snapshots:`: each `packages:` key is itself the
		// resolution, which is what _snapshots_from_packages reconstructs.
		if haveSnapshots && !snapshots[id] {
			continue
		}
		if pkg.platformRestricted {
			continue
		}
		inventory[pkg.name] = ""
	}
	for _, name := range links {
		inventory[name] = ""
	}
	for name, target := range aliases {
		if declared(target) {
			inventory[name] = ""
		}
	}
	return inventory, nil
}

type pnpmPackage struct {
	name               string
	platformRestricted bool
}

// parsePnpmPackages reads the `packages:` section into {name@version: entry}.
func parsePnpmPackages(lines []string) map[string]pnpmPackage {
	body, ok := pnpmSection(lines, "packages")
	if !ok {
		return nil
	}
	packages := make(map[string]pnpmPackage)
	current := ""
	for _, raw := range body {
		indent, stripped, ok := pnpmContentLine(raw)
		if !ok {
			continue
		}
		if indent == 2 {
			current = ""
			if !strings.HasSuffix(stripped, ":") {
				continue
			}
			if name, version := parsePnpmPackageKey(strings.TrimSuffix(stripped, ":")); name != "" && version != "" {
				current = name + "@" + version
				packages[current] = pnpmPackage{name: name}
			}
			continue
		}
		if indent != 4 || current == "" {
			continue
		}
		if key, _, found := strings.Cut(stripped, ":"); found {
			switch strings.TrimSpace(key) {
			case "os", "cpu", "libc":
				entry := packages[current]
				entry.platformRestricted = true
				packages[current] = entry
			}
		}
	}
	return packages
}

// parsePnpmLockNames returns every package name the lockfile mentions: the keys
// of both `packages:` and `snapshots:`, every workspace `link:`, and every npm
// alias. It is the set a resolver may refuse a hub label on -- a name absent
// from it was never installed, so no hub target can carry it.
//
// Deliberately permissive where parsePnpmLockInventory is exact, because the
// two are read for opposite answers. Listing a name the hub does not declare
// only leaves the resolver where it already was; missing one the hub does
// declare would refuse a real dep. So no platform filter, no
// packages/snapshots intersection, and either spelling of a mapping key.
func parsePnpmLockNames(lines []string) map[string]bool {
	names := make(map[string]bool)
	for _, section := range []string{"packages", "snapshots"} {
		body, ok := pnpmSection(lines, section)
		if !ok {
			continue
		}
		for _, raw := range body {
			indent, stripped, ok := pnpmContentLine(raw)
			if !ok || indent != 2 {
				continue
			}
			key := pnpmMappingKey(stripped)
			if key == "" {
				continue
			}
			if name, version := parsePnpmPackageKey(key); name != "" && version != "" {
				names[name] = true
			}
		}
	}
	links, aliases := parsePnpmImporterNames(lines)
	for _, name := range links {
		names[name] = true
	}
	for name := range aliases {
		names[name] = true
	}
	return names
}

// pnpmMappingKey returns the key of a mapping line, whichever way its value is
// written: `foo@1.0.0:` opens a block, `foo@1.0.0: {}` puts the mapping on the
// same line. It returns "" for a line that is not a mapping at all.
func pnpmMappingKey(stripped string) string {
	if strings.HasSuffix(stripped, ":") {
		return strings.TrimSuffix(stripped, ":")
	}
	before, _, found := strings.Cut(stripped, ":")
	if !found {
		return ""
	}
	return before
}

// parsePnpmSnapshotPackageIDs reads the `snapshots:` keys into the set of
// `packages:` ids they resolve, dropping the peer suffix that distinguishes one
// resolution from another. The bool reports whether the section exists at all,
// which is what tells a v9 lockfile from a v6 one.
func parsePnpmSnapshotPackageIDs(lines []string) (map[string]bool, bool) {
	body, ok := pnpmSection(lines, "snapshots")
	if !ok {
		return nil, false
	}
	ids := make(map[string]bool)
	for _, raw := range body {
		indent, stripped, ok := pnpmContentLine(raw)
		if !ok || indent != 2 {
			continue
		}
		key := pnpmMappingKey(stripped)
		if key == "" {
			continue
		}
		if name, version := parsePnpmPackageKey(key); name != "" && version != "" {
			ids[name+"@"+version] = true
		}
	}
	return ids, true
}

// parsePnpmImporterNames reads the `importers:` section for the two dependency
// forms whose hub label no `packages:` entry accounts for: a workspace `link:`,
// which claims the label outright, and an npm alias, which imports a package
// under a name that is not the package's own.
func parsePnpmImporterNames(lines []string) (links []string, aliases map[string]string) {
	body, ok := pnpmSection(lines, "importers")
	if !ok {
		return nil, nil
	}
	aliases = make(map[string]string)
	record := func(name, value string) {
		value = strings.Trim(strings.TrimSpace(value), "'\"")
		if name == "" || value == "" || strings.HasPrefix(value, "file:") {
			return
		}
		if strings.HasPrefix(value, "link:") {
			links = append(links, name)
			return
		}
		if target := pnpmAliasTarget(value); target != "" {
			aliases[name] = target
		}
	}

	section, depName := "", ""
	for _, raw := range body {
		indent, stripped, ok := pnpmContentLine(raw)
		if !ok {
			continue
		}
		if indent == 2 {
			section, depName = "", ""
			continue
		}
		if indent == 4 && strings.HasSuffix(stripped, ":") && !strings.Contains(stripped[:len(stripped)-1], ":") {
			section, depName = strings.TrimSuffix(stripped, ":"), ""
			continue
		}
		switch section {
		case "dependencies", "devDependencies", "optionalDependencies":
		default:
			continue
		}

		if indent == 6 {
			depName = ""
			// v6 inline form:
			//   shared: {specifier: workspace:*, version: link:packages/shared}
			if name, rest, found := strings.Cut(stripped, ":"); found && strings.Contains(rest, "{") {
				if _, after, ok := strings.Cut(rest, "version:"); ok {
					value, _, _ := strings.Cut(after, "}")
					value, _, _ = strings.Cut(value, ",")
					record(strings.Trim(strings.TrimSpace(name), "'\""), value)
				}
				continue
			}
			// v9 form: the dep name alone, specifier/version indented below it.
			if strings.HasSuffix(stripped, ":") {
				depName = strings.Trim(strings.TrimSuffix(stripped, ":"), "'\"")
			}
			continue
		}
		if indent == 8 && depName != "" {
			if key, value, found := strings.Cut(stripped, ":"); found && strings.TrimSpace(key) == "version" {
				record(depName, value)
			}
		}
	}
	return links, aliases
}

// pnpmSection returns the lines under an indent-0 `<name>:` header, stopping at
// the next indent-0 line.
//
// Anchoring is load-bearing, for the reason npm_translate_lock.bzl gives:
// `catalogs:` is the importers idiom one level shallower and `overrides:`
// entries are shaped like package keys, so a reader that scanned the file for
// `specifier:`/`version:` pairs or for `name@version` keys would pull both in.
func pnpmSection(lines []string, name string) ([]string, bool) {
	header := name + ":"
	for i, line := range lines {
		if strings.TrimRight(line, " \t\r") != header {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if indent, _, ok := pnpmContentLine(lines[j]); ok && indent == 0 {
				return lines[i+1 : j], true
			}
		}
		return lines[i+1:], true
	}
	return nil, false
}

// pnpmContentLine reports a line's indent and its trimmed text, and false for a
// line that carries neither (blank or comment).
func pnpmContentLine(raw string) (indent int, stripped string, ok bool) {
	line := strings.TrimRight(raw, " \t\r")
	stripped = strings.TrimSpace(line)
	if stripped == "" || strings.HasPrefix(stripped, "#") {
		return 0, "", false
	}
	return len(line) - len(strings.TrimLeft(line, " ")), stripped, true
}

func pnpmLockfileVersion(lines []string) string {
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if rest, found := strings.CutPrefix(stripped, "lockfileVersion:"); found {
			return strings.Trim(strings.TrimSpace(rest), "'\"")
		}
	}
	return ""
}

// pnpmAliasTarget returns the `name@version` an importer's resolved version
// names, and "" for a plain version. A resolved version is digits and dots;
// only an alias carries the package's own name, and index 0 is skipped so a
// scope's '@' is not the one we find.
func pnpmAliasTarget(version string) string {
	if paren := strings.Index(version, "("); paren != -1 {
		version = version[:paren]
	}
	if len(version) < 2 || strings.Index(version[1:], "@") == -1 {
		return ""
	}
	name, resolved := parsePnpmPackageKey(version)
	if name == "" || resolved == "" {
		return ""
	}
	return name + "@" + resolved
}

// parsePnpmPackageKey splits a lockfile key into (name, version), and returns
// ("", "") for a key that is neither. Ported from _parse_package_key in
// npm/private/npm_translate_lock.bzl: v6 spells the key `/name@version`, v9
// `name@version`, scoped names arrive single-quoted, and a v9 snapshots key
// carries a peer suffix.
func parsePnpmPackageKey(key string) (name, version string) {
	key = strings.TrimSpace(key)
	if len(key) >= 2 && strings.HasPrefix(key, "'") && strings.HasSuffix(key, "'") {
		key = key[1 : len(key)-1]
	}
	key = strings.TrimPrefix(key, "/")
	if paren := strings.Index(key, "("); paren != -1 {
		key = key[:paren]
	}
	if strings.HasPrefix(key, "@") {
		slash := strings.Index(key, "/")
		if slash == -1 {
			return "", ""
		}
		rest := key[slash+1:]
		at := strings.LastIndex(rest, "@")
		if at <= 0 {
			return "", ""
		}
		return key[:slash+1+at], rest[at+1:]
	}
	at := strings.LastIndex(key, "@")
	if at <= 0 || at == len(key)-1 {
		return "", ""
	}
	return key[:at], key[at+1:]
}
