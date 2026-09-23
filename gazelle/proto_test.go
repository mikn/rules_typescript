package typescript

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/label"
	gazelleproto "github.com/bazelbuild/bazel-gazelle/language/proto"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
	bzl "github.com/bazelbuild/buildtools/build"
	"github.com/bazelbuild/rules_go/go/runfiles"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

func protoGazelle(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	binary, err := runfiles.Rlocation(os.Getenv("PROTO_GAZELLE"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, append([]string{"-repo_root=" + root}, args...)...)
	command.Dir = root
	command.Env = append(os.Environ(), "BUILD_WORKSPACE_DIRECTORY="+root)
	b, err := command.CombinedOutput()
	return string(b), err
}

func protoTree() map[string]string {
	return map[string]string{
		"MODULE.bazel": "module(name = \"proto_fixture\")\n",
		"BUILD.bazel":  "# gazelle:exclude ignored\n",
		"schema/BUILD.bazel": `load("@rules_typescript//proto:defs.bzl", "ts_proto_config")
# gazelle:proto_strip_import_prefix /schema
# gazelle:ts_proto plain json
ts_proto_config(
    name = "plain",
    out_dir = "generated/plain",
    tsconfig = "//settings:tsconfig",
    deps = ["@npm//:bufbuild_protobuf"],
)
ts_proto_config(
    name = "json",
    out_dir = "generated/json",
    tsconfig = "//settings:tsconfig",
    deps = ["@npm//:bufbuild_protobuf"],
    options = ["target=ts", "json_types=true"],
)
`,
		"schema/common/common.proto":     `syntax = "proto3"; package example.common; message Common { string value = 1; }`,
		"schema/messages/message.proto":  `syntax = "proto3"; package example.messages; import "common/common.proto"; import "google/protobuf/timestamp.proto"; message Message { example.common.Common common = 1; google.protobuf.Timestamp at = 2; }`,
		"schema/disabled/BUILD.bazel":    "# gazelle:ts_proto none\n",
		"schema/disabled/disabled.proto": `syntax = "proto3"; package example.disabled; message Disabled {}`,
		"ignored/BUILD.bazel":            "# gazelle:ts_proto plain\n",
	}
}

func TestProtoGenerationKeepsSiblingImportsWithinEachOutputIdentity(t *testing.T) {
	root := writeTree(t, protoTree())
	output, err := protoGazelle(t, root, "schema")
	if err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	file := filepath.Join(root, "schema/BUILD.bazel")
	first, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(first)
	for _, want := range []string{`name = "plain/common/example_common_proto"`, `name = "json/messages/example_messages_proto"`, `:plain/common/example_common_proto`, `:json/common/example_common_proto`, `proto = "//schema/messages:example_messages_proto"`, `json_types=true`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %s in\n%s", want, text)
		}
	}
	if strings.Contains(text, "timestamp_proto") || strings.Contains(text, "example_disabled_proto") {
		t.Fatalf("runtime/excluded input became generated sibling:\n%s", text)
	}
	output, err = protoGazelle(t, root, "schema")
	if err != nil {
		t.Fatalf("repeat: %v\n%s", err, output)
	}
	second, _ := os.ReadFile(file)
	if string(first) != string(second) {
		t.Fatalf("repeat generation changed owner:\n%s", second)
	}
	if err := os.Remove(filepath.Join(root, "schema/messages/message.proto")); err != nil {
		t.Fatal(err)
	}
	output, err = protoGazelle(t, root, "schema")
	if err != nil {
		t.Fatalf("removal: %v\n%s", err, output)
	}
	after, _ := os.ReadFile(file)
	if strings.Contains(string(after), "example_messages_proto") {
		t.Fatalf("deleted schema retains wrappers:\n%s", after)
	}
}

func TestProtoPartialUpdatesCannotOverwriteAnIncompleteOwner(t *testing.T) {
	for _, args := range [][]string{{"schema/common"}, {"-r=false", "schema"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			root := writeTree(t, protoTree())
			before := map[string]string{}
			filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					b, _ := os.ReadFile(p)
					before[p] = string(b)
				}
				return nil
			})
			output, err := protoGazelle(t, root, args...)
			if err == nil || !strings.Contains(output, "requires a complete recursive update") {
				t.Fatalf("partial update must fail: %v\n%s", err, output)
			}
			filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					b, _ := os.ReadFile(p)
					if before[p] != string(b) {
						t.Errorf("partial invocation changed %s", p)
					}
				}
				return nil
			})
		})
	}
}

func TestProtoConfigurationCannotSilentlyAcceptExpressionsOrNonTypeScriptOutputs(t *testing.T) {
	for _, input := range []string{
		`ts_proto_config(name="x", out_dir="../escape", tsconfig=":config")`,
		`ts_proto_config(name="x", out_dir="generated", tsconfig=":config", options=["target=js"])`,
		`ts_proto_config(name="x", out_dir="generated", tsconfig=":config", options=[])`,
		`ts_proto_config(name="x", out_dir="generated", tsconfig=select({"//conditions:default": ":config"}))`,
		`ts_proto_config(name="x", out_dir="generated", tsconfig=":config", deps=DEPS)`,
		`ts_proto_config(name="x", out_dir="generated", tsconfig=":config", options=[OPTION])`,
	} {
		f, err := rule.LoadData("BUILD.bazel", "schema", []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseProtoIdentity(f.Rules[0], "schema"); err == nil {
			t.Errorf("accepted unsupported configuration %s", input)
		}
	}
}

func TestProtoExistingWrappersResolveWithoutRegeneratingTheirOwner(t *testing.T) {
	c := emptyConfig()
	(&resolve.Configurer{}).RegisterFlags(nil, "", c)
	c.RepoName = "fixture"
	ts := &tsLang{}
	native := gazelleproto.NewLanguage()
	ix := resolve.NewRuleIndex(func(r *rule.Rule, _ string) resolve.Resolver {
		if r.Kind() == "proto_library" {
			return native
		}
		return ts
	})
	proto := rule.NewRule("proto_library", "message_proto")
	proto.SetAttr("srcs", []string{"message.proto"})
	proto.SetAttr("strip_import_prefix", "/schema")
	ix.AddRule(c, proto, rule.EmptyFile("BUILD.bazel", "schema/messages"))
	for _, id := range []string{"plain", "json"} {
		wrapper := rule.NewRule("ts_proto_library", id+"/messages/message_proto")
		wrapper.SetAttr("proto", "//schema/messages:message_proto")
		wrapper.SetAttr("out_dir", "generated/"+id)
		ix.AddRule(c, wrapper, rule.EmptyFile("BUILD.bazel", "schema"))
	}
	ix.Finish()
	for _, id := range []string{"plain", "json"} {
		got := resolveProtoOutput(c, ix, "schema/generated/"+id+"/messages/message_pb.ts", label.New(c.RepoName, "consumer", "consumer"))
		want := "//schema:" + id + "/messages/message_proto"
		if got != want {
			t.Errorf("%s: got %q, want %q", id, got, want)
		}
	}
}

func TestProtoIndependentOwnersDoNotShareGeneratedSiblings(t *testing.T) {
	tree := protoTree()
	for p, content := range protoTree() {
		if strings.HasPrefix(p, "schema/") {
			tree[strings.Replace(p, "schema/", "other/", 1)] = strings.ReplaceAll(content, "/schema", "/other")
		}
	}
	tree["other/BUILD.bazel"] += "# gazelle:proto_import_prefix alternate\n"
	tree["other/messages/message.proto"] = strings.ReplaceAll(tree["other/messages/message.proto"], `"common/common.proto"`, `"alternate/common/common.proto"`)
	root := writeTree(t, tree)
	output, err := protoGazelle(t, root, ".")
	if err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	for _, owner := range []string{"schema", "other"} {
		b, err := os.ReadFile(filepath.Join(root, owner, "BUILD.bazel"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "//"+owner+"/messages:example_messages_proto") || !strings.Contains(string(b), ":json/common/example_common_proto") {
			t.Fatalf("incorrect owner projection: %s", b)
		}
	}
}

func TestProtoRemovingIdentityDeletesOnlyItsGeneratedWrappers(t *testing.T) {
	root := writeTree(t, protoTree())
	output, err := protoGazelle(t, root, "schema")
	if err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	file := filepath.Join(root, "schema/BUILD.bazel")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := rule.LoadData(file, "schema", b)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range configured.Rules {
		if r.Kind() == "ts_proto_config" {
			r.Delete()
		}
	}
	b = configured.Format()
	lines := strings.Split(string(b), "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, "# gazelle:ts_proto") {
			continue
		}
		kept = append(kept, line)
	}
	text := strings.Replace(strings.Join(kept, "\n"), "ts_proto_library(\n    name = \"plain/common/example_common_proto\",", "# keep\nts_proto_library(\n    name = \"plain/common/example_common_proto\",", 1) + `
ts_proto_library(name = "handwritten", proto = "//schema/common:example_common_proto", out_dir = "custom", tsconfig = "//settings:tsconfig")
`
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	output, err = protoGazelle(t, root, "schema")
	if err != nil {
		t.Fatalf("config removal: %v\n%s", err, output)
	}
	b, err = os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `name = "plain/messages/`) || strings.Contains(string(b), `name = "json/`) || !strings.Contains(string(b), `name = "handwritten"`) || !strings.Contains(string(b), `name = "plain/common/example_common_proto"`) {
		t.Fatalf("identity cleanup corrupted ownership: %s", b)
	}
}

func TestProtoOutputMembershipCannotCreateASecondCompilerOwner(t *testing.T) {
	tc := defaultTsConfig()
	tc.protos.identities["schema:json"] = &protoIdentity{Name: "json", owner: "schema", OutDir: "generated/json"}
	s := storeOf(t, []string{"", "schema", "schema/generated", "schema/generated/json"}, map[string]string{
		"": listingOf("", "consumer.ts", "schema/generated/json/message_pb.ts"),
	})
	got := s.srcs("", tc)
	if len(got.library) != 1 || got.library[0] != "consumer.ts" {
		t.Fatalf("generated output has duplicate compiler membership: %+v", got)
	}
	if root, ok := codegenOutDirOwning("schema/generated/json/nested", tc); !ok || root != "schema/generated/json" {
		t.Fatalf("generated package was not withdrawn: %q %v", root, ok)
	}
}

func TestProtoConflictingNativeOwnersCannotDeclareTheSameOutput(t *testing.T) {
	tree := protoTree()
	tree["schema/other/BUILD.bazel"] = "# gazelle:proto_strip_import_prefix /schema/other\n# gazelle:proto_import_prefix common\n"
	tree["schema/other/common.proto"] = `syntax = "proto3"; package example.other; message Other {}`
	root := writeTree(t, tree)
	output, err := protoGazelle(t, root, "schema")
	if err == nil || !strings.Contains(output, "generate the same proto output") {
		t.Fatalf("conflicting native ownership must fail: %v\n%s", err, output)
	}
	if b, err := os.ReadFile(filepath.Join(root, "schema/BUILD.bazel")); err != nil || string(b) != tree["schema/BUILD.bazel"] {
		t.Fatalf("conflict changed owner BUILD: %s (%v)", b, err)
	}
}

func TestProtoNativeImportOverrideCannotBorrowAnUnrelatedGeneratedOwner(t *testing.T) {
	tree := protoTree()
	tree["schema/other/BUILD.bazel"] = "# gazelle:proto_strip_import_prefix /schema/other\n# gazelle:proto_import_prefix common\n# gazelle:ts_proto none\n"
	tree["schema/other/common.proto"] = `syntax = "proto3"; package example.other; message Other {}`
	tree["schema/messages/BUILD.bazel"] = "# gazelle:resolve proto common/common.proto //schema/other:example_other_proto\n"
	root := writeTree(t, tree)
	output, err := protoGazelle(t, root, "schema")
	if err == nil || !strings.Contains(output, "0 generated providers in identity") {
		t.Fatalf("native override must not borrow a different schema: %v\n%s", err, output)
	}
}

func TestProtoConfigurationWithoutNativeLanguageCannotEraseItsGraph(t *testing.T) {
	c := emptyConfig()
	f := rule.EmptyFile("BUILD.bazel", "schema")
	r := rule.NewRule("ts_proto_config", "plain")
	r.SetAttr("out_dir", "generated")
	r.SetAttr("tsconfig", ":config")
	r.Insert(f)
	err := configureProto(c, "schema", f, defaultTsConfig())
	if err == nil || !strings.Contains(err.Error(), "requires the native proto language") {
		t.Fatalf("missing native owner must fail: %v", err)
	}
}

func TestProtoReusedLanguageCannotCarryAnEarlierInvocationGraph(t *testing.T) {
	language := NewLanguage()
	first := emptyConfig()
	language.RegisterFlags(flag.NewFlagSet("first", flag.ContinueOnError), "update", first)
	old := getConfig(first).protos
	old.identities["first"] = &protoIdentity{Name: "first", owner: "first", OutDir: "generated"}
	old.observations["first"] = []protoObservation{{native: label.New("", "first", "schema_proto")}}
	old.roots = []string{"first"}
	old.graphPath = "earlier.json"
	old.graph = &protoGraph{Roots: map[string]string{"@earlier//:schema": "@@earlier+//:schema"}}

	second := emptyConfig()
	second.WorkDir = second.RepoRoot
	flags := flag.NewFlagSet("second", flag.ContinueOnError)
	language.RegisterFlags(flags, "update", second)
	if err := language.CheckFlags(flags, second); err != nil {
		t.Fatal(err)
	}
	current := getConfig(second).protos
	if current == old || current.graph != nil || current.graphPath != "" || len(current.identities) != 0 || len(current.observations) != 0 || len(current.roots) != 1 || current.roots[0] != "" {
		t.Fatalf("a reused language carried the earlier generation graph: %+v", current)
	}
}

func TestProtoExternalNativeEdgesCannotBeRetargetedByLocalImportOverrides(t *testing.T) {
	c := emptyConfig()
	(&resolve.Configurer{}).RegisterFlags(nil, "", c)
	f := rule.EmptyFile("BUILD.bazel", "")
	f.Directives = []rule.Directive{{Key: "resolve", Value: "proto foreign/b.proto @renamed//:unused"}}
	(&resolve.Configurer{}).Configure(c, "", f)
	file := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(file, []byte(`{"roots":{"@renamed//:a":"@@a+//:a","@renamed//:unused":"@@a+//:unused"},"nodes":[{"label":"@@a+//:a","sources":["foreign/a.proto"],"deps":["@@b+//:b"]},{"label":"@@b+//:b","sources":["foreign/b.proto"]},{"label":"@@a+//:unused","sources":["foreign/b.proto"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	graph, err := loadProtoGraph(file)
	if err != nil {
		t.Fatal(err)
	}
	store := getConfig(c).protos
	store.graph = graph
	id := &protoIdentity{Name: "json", owner: "schema", OutDir: "generated"}
	observations, err := store.externalObservations([]protoObservation{{config: c, native: label.New("", "schema", "local"), identity: id, paths: []string{"local.proto"}, imports: []string{"foreign/a.proto"}}})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, obs := range observations {
		seen[obs.native.String()] = true
		if obs.native.String() == "@@a+//:a" && (len(obs.nativeDeps) != 1 || obs.nativeDeps[0].String() != "@@b+//:b") {
			t.Fatalf("native A dependency was rewritten: %+v", obs.nativeDeps)
		}
	}
	if !seen["@@b+//:b"] || seen["@@a+//:unused"] {
		t.Fatalf("external availability or local override changed native closure: %v", seen)
	}
}

func TestProtoExplicitExternalOverrideCannotBeHiddenByLocalMembership(t *testing.T) {
	c := emptyConfig()
	(&resolve.Configurer{}).RegisterFlags(nil, "", c)
	f := rule.EmptyFile("BUILD.bazel", "")
	f.Directives = []rule.Directive{{Key: "resolve", Value: "proto shared.proto @external//:shared"}}
	(&resolve.Configurer{}).Configure(c, "", f)
	file := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(file, []byte(`{"roots":{"@external//:shared":"@@external+//:shared"},"nodes":[{"label":"@@external+//:shared","sources":["shared.proto"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	graph, err := loadProtoGraph(file)
	if err != nil {
		t.Fatal(err)
	}
	store := getConfig(c).protos
	store.graph = graph
	id := &protoIdentity{Name: "plain", owner: "schema", OutDir: "generated"}
	observations, err := store.externalObservations([]protoObservation{
		{config: c, native: label.New("", "schema", "local"), identity: id, paths: []string{"shared.proto"}},
		{config: c, native: label.New("", "schema", "consumer"), identity: id, paths: []string{"consumer.proto"}, imports: []string{"shared.proto"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, obs := range observations {
		if obs.native.String() == "@@external+//:shared" {
			return
		}
	}
	t.Fatal("local membership hid the explicit external native owner")
}

func TestProtoNativePackageMovesAncestorSourceExportWithoutCompilerProgram(t *testing.T) {
	root := writeTree(t, map[string]string{
		"MODULE.bazel":                `module(name = "native_exports")`,
		"BUILD.bazel":                 "",
		"schema/BUILD.bazel":          `exports_files(["kept.txt", "messages/value.proto"], visibility=["//consumer:__pkg__"], licenses=["notice"])`,
		"schema/kept.txt":             "retained",
		"schema/messages/value.proto": `syntax = "proto3"; package example.messages; message Value { string name = 1; }`,
	})
	output, err := protoGazelle(t, root, "schema")
	if err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	parent := buildFileText(t, root, "schema")
	child := buildFileText(t, root, "schema/messages")
	if strings.Contains(parent, "messages/value.proto") || !strings.Contains(parent, "kept.txt") {
		t.Fatalf("ancestor retains child-owned source:\n%s", parent)
	}
	file, err := rule.LoadFile(filepath.Join(root, "schema/messages/BUILD.bazel"), "schema/messages")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range file.Rules {
		if r.Kind() == "ts_compile" || r.Kind() == "ts_proto_library" {
			t.Fatal("native package invented TypeScript program")
		}
		if r.Kind() != "exports_files" {
			continue
		}
		found = true
		if len(r.Args()) != 1 {
			t.Fatalf("export source argument lost:\n%s", child)
		}
		values, ok := r.Args()[0].(*bzl.ListExpr)
		if !ok || len(values.List) != 1 {
			t.Fatalf("export source list lost:\n%s", child)
		}
		value, ok := values.List[0].(*bzl.StringExpr)
		if !ok || value.Value != "value.proto" || strings.Join(r.AttrStrings("visibility"), ",") != "//consumer:__pkg__" || strings.Join(r.AttrStrings("licenses"), ",") != "notice" {
			t.Fatalf("export contract lost:\n%s", child)
		}
	}
	if !found {
		t.Fatalf("native package lacks source export:\n%s", child)
	}
	output, err = protoGazelle(t, root, "schema")
	if err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	if next := buildFileText(t, root, "schema/messages"); next != child {
		t.Fatalf("native export relocation unstable:\n%s", lineDiff(child, next))
	}
	if next := buildFileText(t, root, "schema"); next != parent {
		t.Fatalf("ancestor relocation unstable:\n%s", lineDiff(parent, next))
	}
}

func TestProtoAmbientDependenciesFollowSelectedConfigInsteadOfWrapperDirectory(t *testing.T) {
	for _, test := range []struct {
		name, selected, configName, configSource string
		wantAmbient                              bool
	}{
		{"generated_config", "//web:tsconfig", "tsconfig", "tsconfig.json", true},
		{"kept_custom_name", "//web:chosen", "chosen", "tsconfig.json", true},
		{"absolute_source", "//web:chosen", "chosen", "//web:tsconfig.json", true},
		{"raw_listed_source", "//web:tsconfig.json", "", "", true},
		{"raw_unlisted_config", "//web:compiler.json", "", "", false},
		{"custom_unlisted_source", "//web:chosen", "chosen", "compiler.json", false},
		{"target_named_like_raw_source", "//web:tsconfig.json", "tsconfig.json", "compiler.json", false},
		{"nonliteral_target_source", "//web:tsconfig.json", "tsconfig.json", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, tc := edgeRepo(t, edgeListings)
			c.RepoName = "fixture"
			tc.programs.programs["web"].Types = []explainfiles.TypeEntry{{Entry: "react", File: storeTypesReact}}
			tc.programs.programs["web"].Implicit = nil
			other := programOf(t, "schema", listingOf("schema", "schema/unrelated.ts"))
			other.Types = []explainfiles.TypeEntry{{Entry: "node", File: storeTypesNode}}
			tc.programs.record(other)
			lang := &tsLang{}
			ix := resolve.NewRuleIndex(func(*rule.Rule, string) resolve.Resolver { return lang })
			if test.configName != "" {
				file, err := rule.LoadData("web/BUILD.bazel", "web", []byte("ts_config(\n name = \""+test.configName+"\", # keep\n src = \""+test.configSource+"\",\n)\n"))
				if err != nil {
					t.Fatal(err)
				}
				if test.configSource == "" {
					file.Rules[0].SetAttr("src", &bzl.Ident{Name: "CONFIG_SOURCE"})
				}
				ix.AddRule(c, file.Rules[0], file)
			}
			ix.Finish()
			id := &protoIdentity{Name: "plain", owner: "schema", Tsconfig: test.selected, Deps: []string{"@npm//:bufbuild_protobuf"}}
			wrapper := rule.NewRule("ts_proto_library", "plain/message_proto")
			from := label.New(c.RepoName, "schema", wrapper.Name())
			want := []string{"@npm//:bufbuild_protobuf"}
			if test.wantAmbient {
				want = append(want, "@npm//web:types_react")
			}
			for range 2 {
				resolveProtoLibrary(c, ix, wrapper, &protoRuleImports{identity: id}, from)
				if got := wrapper.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
					t.Fatalf("deps = %v, want %v", got, want)
				}
			}
		})
	}
}

func TestProtoAmbientSourceDependencySurvivesSeparateOwnerUpdates(t *testing.T) {
	tree := protoTree()
	tree["settings/tsconfig.json"] = `{"compilerOptions":{"module":"ESNext","moduleResolution":"Bundler","target":"ES2022","types":["./ambient"]},"include":["contract.ts"]}`
	tree["settings/ambient.d.ts"] = "declare const generatedAmbient: unique symbol;\n"
	tree["settings/contract.ts"] = "export interface GeneratedContext { token: typeof generatedAmbient }\n"
	root := writeTree(t, tree)
	for _, dir := range []string{"settings", "schema", "schema"} {
		output, err := protoGazelle(t, root, "-ts_verbose", dir)
		if err != nil {
			t.Fatalf("%s: %v\n%s", dir, err, output)
		}
		if dir == "schema" {
			b, _ := os.ReadFile(filepath.Join(root, "schema/BUILD.bazel"))
			if !strings.Contains(string(b), `"//settings"`) {
				settings, _ := os.ReadFile(filepath.Join(root, "settings/BUILD.bazel"))
				t.Fatalf("ambient owner missing: %s\nsettings: %s\nschema: %s", output, settings, b)
			}
		}
	}
}
