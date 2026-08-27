package remix

// The route set is derived the way Remix derives it, not the way the docs
// describe it: this is a port of @remix-run/dev's dist/config/flat-routes.js
// (v2.17), including the parts that are easy to get subtly wrong -- the segment
// state machine, one level of readdir, folder routes, and the longest-prefix
// parenting.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// routesPrefix is the app-relative directory Remix reads routes from, and the
// prefix every route id carries.
const routesPrefix = "routes"

// routeModuleExts are the extensions Remix accepts for a route module. .md and
// .mdx are routes to Remix and are reported as unsupported here rather than
// dropped, because a route missing from the set silently changes the URL tree.
var routeModuleExts = []string{".js", ".jsx", ".ts", ".tsx", ".md", ".mdx"}

// Route is one entry of the manifest Remix's file conventions produce.
type Route struct {
	// ID is the app-relative path of the route module without its extension,
	// or the directory path for a folder route: "routes/dash.settings".
	ID string

	// File is the app-relative path of the route module itself.
	File string

	// Path is the URL path relative to the parent route, as Remix stores it.
	// Empty when the route contributes no segment of its own.
	Path string

	// FullPath is the URL path before the parent's is subtracted, which is what
	// a reader wants to see next to a target.
	FullPath string

	// Index marks an _index route.
	Index bool

	// ParentID is the id of the nearest ancestor route, "root" at the top.
	ParentID string

	// Dir is the app-relative directory of a folder route, empty for a flat
	// file. A folder route's colocated files are invisible to routing but must
	// still reach the build.
	Dir string
}

// URL returns the absolute URL the route answers on.
func (r Route) URL() string {
	if r.FullPath == "" {
		return "/"
	}
	return "/" + r.FullPath
}

// Manifest walks appDir/routes one level deep and returns the routes Remix's
// file conventions produce, plus the diagnostics Remix itself would report.
// A missing routes directory yields no routes and no diagnostics.
func Manifest(appDir string) ([]Route, []string) {
	routesDir := filepath.Join(appDir, routesPrefix)
	entries, err := os.ReadDir(routesDir)
	if err != nil {
		return nil, nil
	}

	var diags []string
	var found []Route

	for _, entry := range entries {
		name := entry.Name()
		// Remix's ignore list always contains "**/.*".
		if strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() {
			file, conflict := folderRouteModule(filepath.Join(routesDir, name))
			if conflict != "" {
				diags = append(diags, conflict)
			}
			if file == "" {
				continue
			}
			found = append(found, Route{
				ID:   path.Join(routesPrefix, name),
				File: path.Join(routesPrefix, name, file),
				Dir:  path.Join(routesPrefix, name),
			})
			continue
		}
		if !isRouteModule(name) {
			continue
		}
		found = append(found, Route{
			ID:   path.Join(routesPrefix, strings.TrimSuffix(name, path.Ext(name))),
			File: path.Join(routesPrefix, name),
		})
	}

	found, idDiags := dropIDConflicts(found)
	diags = append(diags, idDiags...)

	// Longest id first: that is the order the parenting and the path
	// subtraction below both depend on.
	sort.SliceStable(found, func(i, j int) bool {
		return len(found[i].ID) > len(found[j].ID)
	})

	routes := make([]Route, 0, len(found))
	for _, r := range found {
		r.Index = strings.HasSuffix(r.ID, "_index")
		segments, raw, err := getRouteSegments(strings.TrimPrefix(r.ID, routesPrefix+"/"))
		if err != nil {
			diags = append(diags, err.Error())
			continue
		}
		r.FullPath = createRoutePath(segments, raw, r.Index)
		routes = append(routes, r)
	}

	assignParents(routes)
	subtractParentPaths(routes)
	diags = append(diags, urlConflicts(routes)...)
	return routes, diags
}

// folderRouteModule returns the route module of a directory route: route.* wins
// over index.*, and both present is the conflict Remix reports.
func folderRouteModule(dir string) (file, diag string) {
	route := findConfig(dir, "route")
	index := findConfig(dir, "index")
	if route != "" && index != "" {
		diag = fmt.Sprintf(
			"folder route %s declares both %s and %s; Remix uses %s and reports the other as a path conflict",
			filepath.Base(dir), route, index, route)
	}
	if route != "" {
		return route, diag
	}
	return index, diag
}

// findConfig returns the first basename+ext that exists in dir, in Remix's
// extension order.
func findConfig(dir, basename string) string {
	for _, ext := range routeModuleExts {
		name := basename + ext
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return name
		}
	}
	return ""
}

func isRouteModule(name string) bool {
	ext := path.Ext(name)
	for _, candidate := range routeModuleExts {
		if ext == candidate {
			return true
		}
	}
	return false
}

// dropIDConflicts keeps the first file claiming each route id, which is what
// Remix does before it reports the rest.
func dropIDConflicts(found []Route) ([]Route, []string) {
	seen := map[string]string{}
	conflicts := map[string][]string{}
	kept := make([]Route, 0, len(found))
	for _, r := range found {
		if first, ok := seen[r.ID]; ok {
			if len(conflicts[r.ID]) == 0 {
				conflicts[r.ID] = []string{first}
			}
			conflicts[r.ID] = append(conflicts[r.ID], r.File)
			continue
		}
		seen[r.ID] = r.File
		kept = append(kept, r)
	}
	ids := make([]string, 0, len(conflicts))
	for id := range conflicts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	diags := make([]string, 0, len(ids))
	for _, id := range ids {
		diags = append(diags, fmt.Sprintf(
			"route id %q is claimed by more than one file (%s); Remix keeps the first and drops the rest",
			id, strings.Join(conflicts[id], ", ")))
	}
	return kept, diags
}

// assignParents gives each route the longest already-seen ancestor whose id it
// extends on a "." or "/" boundary, and "root" when there is none. routes must
// be sorted longest id first.
func assignParents(routes []Route) {
	claimed := make([]bool, len(routes))
	for i := range routes {
		for j := 0; j < i; j++ {
			if claimed[j] {
				continue
			}
			rest := strings.TrimPrefix(routes[j].ID, routes[i].ID)
			if len(rest) == len(routes[j].ID) || rest == "" {
				continue
			}
			if rest[0] != '.' && rest[0] != '/' {
				continue
			}
			routes[j].ParentID = routes[i].ID
			claimed[j] = true
		}
	}
	for i := range routes {
		if routes[i].ParentID == "" {
			routes[i].ParentID = "root"
		}
	}
}

// subtractParentPaths turns each FullPath into the parent-relative Path Remix
// stores. Children come first in the ordering, so a parent's FullPath is still
// intact when its children read it.
func subtractParentPaths(routes []Route) {
	full := make(map[string]string, len(routes))
	for _, r := range routes {
		full[r.ID] = r.FullPath
	}
	for i := range routes {
		routes[i].Path = routes[i].FullPath
		parent := full[routes[i].ParentID]
		if parent == "" || routes[i].FullPath == "" {
			continue
		}
		routes[i].Path = strings.Trim(strings.TrimPrefix(routes[i].FullPath, parent), "/")
	}
}

// urlConflicts reports two routes answering on the same URL. A pathless layout
// route is exempt: several of them on the same path is the point of them.
func urlConflicts(routes []Route) []string {
	seen := map[string]string{}
	var diags []string
	for _, r := range routes {
		last := strings.TrimPrefix(r.ID, routesPrefix+"/")
		if idx := strings.LastIndex(last, "."); idx >= 0 {
			last = last[idx+1:]
		}
		if strings.HasPrefix(last, "_") && last != "_index" {
			continue
		}
		if r.FullPath == "" && !r.Index {
			continue
		}
		key := r.FullPath
		if r.Index {
			key += "?index"
		}
		if first, ok := seen[key]; ok {
			diags = append(diags, fmt.Sprintf(
				"%s and %s both answer on %s; Remix keeps the first and drops the rest",
				first, r.File, r.URL()))
			continue
		}
		seen[key] = r.File
	}
	sort.Strings(diags)
	return diags
}

// ---- segment parsing -------------------------------------------------------

func isSegmentSeparator(c byte) bool {
	return c == '/' || c == '.' || c == '\\'
}

// segment parser states, named as flat-routes.js names them.
const (
	stNormal = iota
	stEscape
	stOptional
	stOptionalEscape
)

// getRouteSegments splits a route id (with the "routes/" prefix removed) into
// URL segments and the raw text each came from. The raw text is what decides
// whether a leading "_" or trailing "_" was written by the author or produced
// by an escape, which is why both are carried.
func getRouteSegments(routeID string) (segments, raw []string, err error) {
	state := stNormal
	var seg, rawSeg strings.Builder

	push := func() error {
		s, r := seg.String(), rawSeg.String()
		seg.Reset()
		rawSeg.Reset()
		if s == "" {
			return nil
		}
		for _, bad := range []string{"*", ":"} {
			if strings.Contains(r, bad) {
				return fmt.Errorf("route segment %q of %q cannot contain %q; React Router has no syntax for it", r, routeID, bad)
			}
		}
		if strings.Contains(s, "/") {
			return fmt.Errorf("route segment %q of %q cannot contain %q; React Router has no syntax for it", s, routeID, "/")
		}
		segments = append(segments, s)
		raw = append(raw, r)
		return nil
	}

	for i := 0; i < len(routeID); {
		ch := routeID[i]
		i++
		last := i == len(routeID)

		switch state {
		case stNormal:
			switch {
			case isSegmentSeparator(ch):
				if err := push(); err != nil {
					return nil, nil, err
				}
			case ch == '[':
				state = stEscape
				rawSeg.WriteByte(ch)
			case ch == '(':
				state = stOptional
				rawSeg.WriteByte(ch)
			case seg.Len() == 0 && ch == '$':
				seg.WriteString(paramOrSplat(last))
				rawSeg.WriteByte(ch)
			default:
				seg.WriteByte(ch)
				rawSeg.WriteByte(ch)
			}
		case stEscape:
			if ch == ']' {
				state = stNormal
				rawSeg.WriteByte(ch)
				continue
			}
			seg.WriteByte(ch)
			rawSeg.WriteByte(ch)
		case stOptional:
			switch {
			case ch == ')':
				seg.WriteString("?")
				rawSeg.WriteByte(ch)
				state = stNormal
			case ch == '[':
				state = stOptionalEscape
				rawSeg.WriteByte(ch)
			case seg.Len() == 0 && ch == '$':
				seg.WriteString(paramOrSplat(last))
				rawSeg.WriteByte(ch)
			default:
				seg.WriteByte(ch)
				rawSeg.WriteByte(ch)
			}
		case stOptionalEscape:
			if ch == ']' {
				state = stOptional
				rawSeg.WriteByte(ch)
				continue
			}
			seg.WriteByte(ch)
			rawSeg.WriteByte(ch)
		}
	}
	if err := push(); err != nil {
		return nil, nil, err
	}
	return segments, raw, nil
}

func paramOrSplat(last bool) string {
	if last {
		return "*"
	}
	return ":"
}

// createRoutePath joins the segments into a URL path, dropping the trailing
// segment of an index route, skipping pathless layout segments (leading "_")
// and honouring the parent opt-out (trailing "_").
func createRoutePath(segments, raw []string, isIndex bool) string {
	if isIndex && len(segments) > 0 {
		segments = segments[:len(segments)-1]
		raw = raw[:len(raw)-1]
	}
	var out []string
	for i, segment := range segments {
		rawSegment := raw[i]
		if strings.HasPrefix(segment, "_") && strings.HasPrefix(rawSegment, "_") {
			continue
		}
		if strings.HasSuffix(segment, "_") && strings.HasSuffix(rawSegment, "_") {
			segment = segment[:len(segment)-1]
		}
		out = append(out, segment)
	}
	return strings.Join(out, "/")
}
