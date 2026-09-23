package typescript

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/language"
	gazelleproto "github.com/bazelbuild/bazel-gazelle/language/proto"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
	bzl "github.com/bazelbuild/buildtools/build"
	"github.com/bazelbuild/rules_go/go/runfiles"
)

var protoRuntimeImportsRlocationpath string

func (s *protoStore) loadRuntimeImports() error {
	if s.runtimeImports != nil {
		return nil
	}
	p, err := runfiles.Rlocation(protoRuntimeImportsRlocationpath)
	if err != nil {
		return fmt.Errorf("protobuf runtime import metadata is missing from Gazelle runfiles: %w", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &s.runtimeImports)
}

type protoIdentity struct {
	Name        string
	OutDir      string
	Tsconfig    string
	NodeModules string
	Deps        []string
	Options     []string
	owner       string
}

type protoStore struct {
	graphPath      string
	graph          *protoGraph
	roots          []string
	recursive      bool
	identities     map[string]*protoIdentity
	observations   map[string][]protoObservation
	runtimeImports map[string]string
}

type protoObservation struct {
	nativeDeps []label.Label
	config     *config.Config
	native     label.Label
	identity   *protoIdentity
	paths      []string
	imports    []string
}

type protoRuleImports struct {
	nativeDeps []label.Label
	config     *config.Config
	identity   *protoIdentity
	imports    []string
}

func newProtoStore() *protoStore {
	return &protoStore{recursive: true, identities: map[string]*protoIdentity{}, observations: map[string][]protoObservation{}}
}

func within(dir, root string) bool {
	return dir == root || root == "" || strings.HasPrefix(dir, root+"/")
}

func (s *protoStore) checkScope(c *config.Config, owner string) error {
	if s.roots == nil {
		return nil
	}
	affected, complete := false, false
	for _, root := range s.roots {
		if within(root, owner) {
			affected = true
		}
		if s.recursive && within(owner, root) {
			affected = true
			complete = true
		}
	}
	if affected && !complete {
		return fmt.Errorf("typescript proto owner //%s requires a complete recursive update; rerun gazelle -repo_root=%s -r=true %s", owner, c.RepoRoot, filepath.Join(c.RepoRoot, filepath.FromSlash(owner)))
	}
	return nil
}

var protoIdentityName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

func parseProtoIdentity(r *rule.Rule, owner string) (*protoIdentity, error) {
	id := protoIdentity{Name: r.Name(), Options: []string{"target=ts"}}
	for key, dst := range map[string]*string{"out_dir": &id.OutDir, "tsconfig": &id.Tsconfig, "node_modules": &id.NodeModules} {
		expr := r.Attr(key)
		if expr == nil {
			continue
		}
		value, ok := expr.(*bzl.StringExpr)
		if !ok {
			return nil, fmt.Errorf("ts_proto_config %s: %s must be a literal string", id.Name, key)
		}
		*dst = value.Value
	}
	for key, dst := range map[string]*[]string{"deps": &id.Deps, "options": &id.Options} {
		expr := r.Attr(key)
		if expr == nil {
			continue
		}
		list, ok := expr.(*bzl.ListExpr)
		if !ok {
			return nil, fmt.Errorf("ts_proto_config %s: %s must be a literal string list", id.Name, key)
		}
		*dst = []string{}
		for _, expr := range list.List {
			value, ok := expr.(*bzl.StringExpr)
			if !ok {
				return nil, fmt.Errorf("ts_proto_config %s: %s must contain literal strings", id.Name, key)
			}
			*dst = append(*dst, value.Value)
		}
	}
	if !protoIdentityName.MatchString(id.Name) || id.OutDir == "" || id.Tsconfig == "" {
		return nil, fmt.Errorf("ts_proto_config requires name, package-relative out_dir and tsconfig")
	}
	if path.IsAbs(id.OutDir) || path.Clean(id.OutDir) != id.OutDir || id.OutDir == "." || id.OutDir == ".." || strings.HasPrefix(id.OutDir, "../") {
		return nil, fmt.Errorf("ts_proto_config %s: out_dir must stay inside the owner package", id.Name)
	}
	if !slices.Contains(id.Options, "target=ts") {
		return nil, fmt.Errorf("ts_proto_config %s: options must select target=ts", id.Name)
	}
	for _, opt := range id.Options {
		if strings.HasPrefix(opt, "target=") && opt != "target=ts" {
			return nil, fmt.Errorf("ts_proto_config %s: conflicting target option %s", id.Name, opt)
		}
	}
	for _, raw := range append(append([]string{id.Tsconfig}, id.Deps...), id.NodeModules) {
		if raw == "" {
			continue
		}
		if _, err := label.Parse(raw); err != nil {
			return nil, fmt.Errorf("ts_proto_config %s: invalid label %q: %w", id.Name, raw, err)
		}
	}
	id.owner = owner
	return &id, nil
}

func configureProto(c *config.Config, rel string, f *rule.File, tc *tsConfig) error {
	if f == nil {
		return nil
	}
	for _, r := range f.Rules {
		if managedProtoWrapper(c, rel, r) {
			if gazelleproto.GetProtoConfig(c) == nil {
				return fmt.Errorf("typescript proto generation requires the native proto language in the Gazelle binary")
			}
			if err := tc.protos.checkScope(c, rel); err != nil {
				return err
			}
		}
	}
	for _, r := range f.Rules {
		if r.Kind() != "ts_proto_config" {
			continue
		}
		if gazelleproto.GetProtoConfig(c) == nil {
			return fmt.Errorf("typescript proto generation requires the native proto language in the Gazelle binary")
		}
		if err := tc.protos.loadRuntimeImports(); err != nil {
			return err
		}
		if err := tc.protos.checkScope(c, rel); err != nil {
			return err
		}
		id, err := parseProtoIdentity(r, rel)
		if err != nil {
			return err
		}
		if _, ok := tc.protoIdentities[id.Name]; ok {
			return fmt.Errorf("ts_proto_config: identity %s is already defined; use a distinct name", id.Name)
		}
		for _, other := range tc.protos.identities {
			if other.owner == rel && (within(id.OutDir, other.OutDir) || within(other.OutDir, id.OutDir)) {
				return fmt.Errorf("ts_proto_config: output roots for %s and %s overlap; use distinct roots", id.Name, other.Name)
			}
		}
		tc.protoIdentities[id.Name] = id
		tc.protos.identities[rel+":"+id.Name] = id
	}
	for _, d := range f.Directives {
		switch d.Key {
		case "ts_proto":
			tc.protoEnabled = nil
			for _, name := range strings.Fields(d.Value) {
				if name == "none" {
					if len(strings.Fields(d.Value)) != 1 {
						return fmt.Errorf("ts_proto: none cannot be combined with identities")
					}
					break
				}
				if tc.protoIdentities[name] == nil {
					return fmt.Errorf("ts_proto: undefined identity %s; define it with ts_proto_config on the schema ancestor", name)
				}
				if slices.Contains(tc.protoEnabled, name) {
					return fmt.Errorf("ts_proto: duplicate identity %s", name)
				}
				tc.protoEnabled = append(tc.protoEnabled, name)
			}
		}
	}
	return nil
}

func protoWrapperName(native label.Label, id *protoIdentity) string {
	rel := strings.TrimPrefix(strings.TrimPrefix(native.Pkg, id.owner), "/")
	if native.Canonical {
		rel = path.Join("_external", native.Repo, native.Pkg)
	}
	return path.Join(id.Name, rel, native.Name)
}

func absoluteProtoLabel(c *config.Config, native label.Label, pkg string) label.Label {
	native = getConfig(c).protos.graph.normalize(native)
	native = native.Abs(c.RepoName, pkg)
	if native.Repo == "" {
		native.Repo = c.RepoName
	}
	return native
}

func managedProtoWrapper(c *config.Config, owner string, r *rule.Rule) bool {
	if r.Kind() != "ts_proto_library" {
		return false
	}
	family, _, ok := strings.Cut(r.Name(), "/")
	if !ok || !protoIdentityName.MatchString(family) {
		return false
	}
	native, err := label.Parse(r.AttrString("proto"))
	if err != nil {
		return false
	}
	native = absoluteProtoLabel(c, native, owner)
	return ((native.Repo == c.RepoName && within(native.Pkg, owner)) || native.Canonical) && r.Name() == protoWrapperName(native, &protoIdentity{Name: family, owner: owner})
}

func withProtoRules(args language.GenerateArgs, tc *tsConfig, res language.GenerateResult) language.GenerateResult {
	nativeLanguage := gazelleproto.NewLanguage()
	for _, native := range args.OtherGen {
		if native.Kind() != "proto_library" {
			continue
		}
		pkg, ok := native.PrivateAttr(gazelleproto.PackageKey).(gazelleproto.Package)
		if !ok {
			continue
		}
		specs := nativeLanguage.Imports(args.Config, native, &rule.File{Pkg: args.Rel})
		var paths []string
		for _, spec := range specs {
			paths = append(paths, spec.Imp)
		}
		if len(paths) == 0 {
			continue
		}
		imports := slices.Sorted(maps.Keys(pkg.Imports))
		for _, name := range tc.protoEnabled {
			id := tc.protoIdentities[name]
			tc.protos.observations[id.owner] = append(tc.protos.observations[id.owner], protoObservation{config: args.Config, native: label.New(args.Config.RepoName, args.Rel, native.Name()), identity: id, paths: paths, imports: imports})
		}
	}
	generated := map[string]bool{}
	outputs := map[string]string{}
	observations, err := tc.protos.externalObservations(tc.protos.observations[args.Rel])
	if err != nil {
		log.Fatal(err)
	}
	for _, obs := range observations {
		id := obs.identity
		name := protoWrapperName(obs.native, id)
		if generated[name] {
			log.Fatalf("typescript: duplicate generated proto library %s; check native proto ownership", name)
		}
		generated[name] = true
		for _, source := range obs.paths {
			output := path.Join(id.OutDir, source)
			if previous, ok := outputs[output]; ok {
				log.Fatalf("typescript: %s and %s generate the same proto output %s; select one native owner", previous, name, output)
			}
			outputs[output] = name
		}
		r := rule.NewRule("ts_proto_library", name)
		r.SetAttr("visibility", []string{"//visibility:public"})
		r.SetAttr("proto", obs.native.Rel(args.Config.RepoName, args.Rel).String())
		r.SetAttr("out_dir", id.OutDir)
		r.SetAttr("tsconfig", id.Tsconfig)
		r.SetAttr("options", id.Options)
		if id.NodeModules != "" {
			r.SetAttr("node_modules", id.NodeModules)
		}
		if len(id.Deps) > 0 {
			r.SetAttr("deps", id.Deps)
		}
		r.SetPrivateAttr("_ts_proto_imports", &protoRuleImports{config: obs.config, identity: id, imports: obs.imports, nativeDeps: obs.nativeDeps})
		res.Gen = append(res.Gen, r)
		res.Imports = append(res.Imports, r.PrivateAttr("_ts_proto_imports"))
	}
	if args.File != nil {
		for _, r := range args.File.Rules {
			if managedProtoWrapper(args.Config, args.Rel, r) && !generated[r.Name()] {
				res.Empty = append(res.Empty, rule.NewRule(r.Kind(), r.Name()))
			}
		}
	}
	return res
}

func protoWrapperKey(root string, native label.Label) string {
	return "ts_proto:" + root + ":" + native.String()
}

func protoImportsForRule(c *config.Config, r *rule.Rule, f *rule.File) []resolve.ImportSpec {
	native, err := label.Parse(r.AttrString("proto"))
	if err != nil {
		return nil
	}
	root := path.Join(f.Pkg, r.AttrString("out_dir"))
	return []resolve.ImportSpec{
		{Lang: languageName, Imp: "ts_proto_root:" + root},
		{Lang: languageName, Imp: protoWrapperKey(root, absoluteProtoLabel(c, native, f.Pkg))},
	}
}

func protoProvider(c *config.Config, ix *resolve.RuleIndex, root, imp string) []resolve.FindResult {
	spec := resolve.ImportSpec{Lang: "proto", Imp: imp}
	var native []resolve.FindResult
	if overridden, ok := resolve.FindRuleWithOverride(c, spec, "proto"); ok {
		for _, node := range getConfig(c).protos.graph.providers(c, imp) {
			native = append(native, resolve.FindResult{Label: getConfig(c).protos.graph.labels[node.Label]})
		}
		if len(native) == 0 {
			native = []resolve.FindResult{{Label: absoluteProtoLabel(c, overridden, "")}}
		}
	} else {
		native = ix.FindRulesByImportWithConfig(c, spec, "proto")
		if len(native) == 0 {
			for _, node := range getConfig(c).protos.graph.providers(c, imp) {
				native = append(native, resolve.FindResult{Label: getConfig(c).protos.graph.labels[node.Label]})
			}
		}
	}
	var providers []resolve.FindResult
	for _, n := range native {
		providers = append(providers, ix.FindRulesByImport(resolve.ImportSpec{Lang: languageName, Imp: protoWrapperKey(root, n.Label)}, languageName)...)
	}
	return providers
}

func resolveProtoOutput(c *config.Config, ix *resolve.RuleIndex, file string, from label.Label) string {
	if !strings.HasSuffix(file, "_pb.ts") {
		return ""
	}
	for dir := path.Dir(file); dir != "." && dir != "/" && dir != ""; dir = path.Dir(dir) {
		if len(ix.FindRulesByImport(resolve.ImportSpec{Lang: languageName, Imp: "ts_proto_root:" + dir}, languageName)) == 0 {
			continue
		}
		imp := strings.TrimSuffix(strings.TrimPrefix(file, dir+"/"), "_pb.ts") + ".proto"
		providers := protoProvider(c, ix, dir, imp)
		if len(providers) != 1 {
			log.Fatalf("typescript: %s has %d generated providers for %s; select exactly one native owner in %s", from.String(), len(providers), file, dir)
		}
		return providers[0].Label.Rel(from.Repo, from.Pkg).String()
	}
	return ""
}

func resolveProtoLibrary(c *config.Config, ix *resolve.RuleIndex, r *rule.Rule, imps *protoRuleImports, from label.Label) {
	deps := map[string]bool{}
	for _, dep := range imps.identity.Deps {
		deps[dep] = true
	}
	tc := getConfig(c)
	reported := map[string]bool{}
	for _, edge := range configTypeEdges(c, ix, imps.identity.Tsconfig, imps.identity.owner) {
		if dep := edgeDep(c, ix, tc, edge, from, reported); dep != "" {
			deps[dep] = true
		}
	}
	for _, native := range imps.nativeDeps {
		found := ix.FindRulesByImport(resolve.ImportSpec{Lang: languageName, Imp: protoWrapperKey(path.Join(imps.identity.owner, imps.identity.OutDir), native)}, languageName)
		if len(found) != 1 {
			log.Fatalf("typescript: %s: native dependency %s has %d generated providers in identity %s", from.String(), native.String(), len(found), imps.identity.Name)
		}
		if found[0].Label != from {
			deps[found[0].Label.Rel(from.Repo, from.Pkg).String()] = true
		}
	}
	for _, imp := range imps.imports {
		if getConfig(c).protos.runtimeImports[imp] != "" && !slices.Contains(imps.identity.Options, "bootstrap_wkt=true") {
			continue
		}
		found := protoProvider(imps.config, ix, path.Join(imps.identity.owner, imps.identity.OutDir), imp)
		if len(found) != 1 {
			log.Fatalf("typescript: %s: proto import %s has %d generated providers in identity %s; enable exactly one native owner for that identity", from.String(), imp, len(found), imps.identity.Name)
		}
		if found[0].Label != from {
			deps[found[0].Label.Rel(from.Repo, from.Pkg).String()] = true
		}
	}
	if len(deps) > 0 {
		r.SetAttr("deps", slices.Sorted(maps.Keys(deps)))
	} else {
		r.DelAttr("deps")
	}
}
