package tanstack

import (
	"testing"

	"github.com/bazelbuild/bazel-gazelle/language"
)

// ---- routePatternFromRel tests ---------------------------------------------

func TestRoutePatternFromRel_Root(t *testing.T) {
	got := routePatternFromRel("")
	want := "/"
	if got != want {
		t.Errorf("routePatternFromRel(%q): got %q, want %q", "", got, want)
	}
}

func TestRoutePatternFromRel_Index(t *testing.T) {
	got := routePatternFromRel("index.tsx")
	want := "/"
	if got != want {
		t.Errorf("routePatternFromRel(%q): got %q, want %q", "index.tsx", got, want)
	}
}

func TestRoutePatternFromRel_StaticRoute(t *testing.T) {
	cases := []struct {
		rel  string
		want string
	}{
		{"about.tsx", "/about"},
		{"users.tsx", "/users"},
		{"settings/profile.tsx", "/settings/profile"},
		{"admin/dashboard.tsx", "/admin/dashboard"},
	}
	for _, tc := range cases {
		got := routePatternFromRel(tc.rel)
		if got != tc.want {
			t.Errorf("routePatternFromRel(%q): got %q, want %q", tc.rel, got, tc.want)
		}
	}
}

func TestRoutePatternFromRel_DynamicSegment(t *testing.T) {
	cases := []struct {
		rel  string
		want string
	}{
		{"$userId.tsx", "/:userId"},
		{"users/$userId.tsx", "/users/:userId"},
		{"users/$userId/posts/$postId.tsx", "/users/:userId/posts/:postId"},
		{"$id/edit.tsx", "/:id/edit"},
	}
	for _, tc := range cases {
		got := routePatternFromRel(tc.rel)
		if got != tc.want {
			t.Errorf("routePatternFromRel(%q): got %q, want %q", tc.rel, got, tc.want)
		}
	}
}

func TestRoutePatternFromRel_RootLayout(t *testing.T) {
	// __root has no URL segment; the directory produces "/" as the pattern.
	got := routePatternFromRel("__root.tsx")
	want := "/"
	if got != want {
		t.Errorf("routePatternFromRel(%q): got %q, want %q", "__root.tsx", got, want)
	}
}

// ---- dynamicSegmentsFromRel tests ------------------------------------------

func TestDynamicSegmentsFromRel_NoDynamic(t *testing.T) {
	got := dynamicSegmentsFromRel("users/profile.tsx")
	if len(got) != 0 {
		t.Errorf("expected no dynamic segments, got: %v", got)
	}
}

func TestDynamicSegmentsFromRel_SingleParam(t *testing.T) {
	got := dynamicSegmentsFromRel("users/$userId.tsx")
	want := []string{"userId"}
	assertStringSlice(t, "single param", got, want)
}

func TestDynamicSegmentsFromRel_MultipleParams(t *testing.T) {
	got := dynamicSegmentsFromRel("users/$userId/posts/$postId.tsx")
	want := []string{"userId", "postId"}
	assertStringSlice(t, "multiple params", got, want)
}

// ---- relativeToRoutesRoot tests --------------------------------------------

func TestRelativeToRoutesRoot_Direct(t *testing.T) {
	// A file directly in routes/ — the returned rel is just the file basename.
	got := relativeToRoutesRoot("src/routes/about.tsx")
	want := "about.tsx"
	if got != want {
		t.Errorf("relativeToRoutesRoot: got %q, want %q", got, want)
	}
}

func TestRelativeToRoutesRoot_Nested(t *testing.T) {
	got := relativeToRoutesRoot("src/routes/users/$userId")
	want := "users/$userId"
	if got != want {
		t.Errorf("relativeToRoutesRoot: got %q, want %q", got, want)
	}
}

func TestRelativeToRoutesRoot_NoRoutes(t *testing.T) {
	got := relativeToRoutesRoot("src/components/Button")
	if got != "" {
		t.Errorf("expected empty string for path outside routes/, got: %q", got)
	}
}

func TestRelativeToRoutesRoot_RoutesRoot(t *testing.T) {
	// The routes/ directory itself.
	got := relativeToRoutesRoot("src/routes")
	if got != "" {
		t.Errorf("expected empty string for the routes/ directory itself, got: %q", got)
	}
}

// ---- isInsideRoutesDir tests -----------------------------------------------

func TestIsInsideRoutesDir(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{"src/routes", true},
		{"src/routes/about", true},
		{"src/routes/users/$userId", true},
		{"app/routes/posts", true},
		{"src/components", false},
		{"", false},
		{"routes", true}, // routes/ itself
	}
	for _, tc := range cases {
		got := isInsideRoutesDir(tc.rel)
		if got != tc.want {
			t.Errorf("isInsideRoutesDir(%q): got %v, want %v", tc.rel, got, tc.want)
		}
	}
}

// ---- NearestRoutesPackage tests --------------------------------------------

func TestNearestRoutesPackage(t *testing.T) {
	cases := []struct {
		rel  string
		want string
	}{
		{"src/routes", "src/routes"},
		{"src/routes/users/$userId", "src/routes"},
		{"src/components", ""},
		{"routes/about", "routes"},
	}
	for _, tc := range cases {
		got := NearestRoutesPackage(tc.rel)
		if got != tc.want {
			t.Errorf("NearestRoutesPackage(%q): got %q, want %q", tc.rel, got, tc.want)
		}
	}
}

// ---- RoutePatternForFile tests (public API) --------------------------------

func TestRoutePatternForFile(t *testing.T) {
	cases := []struct {
		wsPath string
		want   string
	}{
		{"src/routes/index.tsx", "/"},
		{"src/routes/about.tsx", "/about"},
		{"src/routes/users/$userId.tsx", "/users/:userId"},
		{"src/routes/__root.tsx", "/"},
		{"src/components/Button.tsx", ""}, // not in routes/
	}
	for _, tc := range cases {
		got := RoutePatternForFile(tc.wsPath)
		if got != tc.want {
			t.Errorf("RoutePatternForFile(%q): got %q, want %q", tc.wsPath, got, tc.want)
		}
	}
}

// ---- buildRouteInfo tests --------------------------------------------------

func TestBuildRouteInfo_RootLayout(t *testing.T) {
	args := language.GenerateArgs{
		Rel:          "src/routes",
		RegularFiles: []string{"__root.tsx", "index.tsx"},
	}
	info := buildRouteInfo(args)
	if !info.IsRootLayout {
		t.Error("expected IsRootLayout = true when __root.tsx is present")
	}
	if info.RoutePattern != "" {
		t.Errorf("root layout should have empty RoutePattern, got: %q", info.RoutePattern)
	}
}

func TestBuildRouteInfo_StaticRoute(t *testing.T) {
	args := language.GenerateArgs{
		Rel:          "src/routes/about",
		RegularFiles: []string{"about.tsx"},
	}
	info := buildRouteInfo(args)
	if info.IsRootLayout {
		t.Error("expected IsRootLayout = false")
	}
	if info.RoutePattern != "/about" {
		t.Errorf("expected RoutePattern = /about, got: %q", info.RoutePattern)
	}
	if len(info.DynamicSegments) != 0 {
		t.Errorf("expected no dynamic segments, got: %v", info.DynamicSegments)
	}
}

func TestBuildRouteInfo_DynamicRoute(t *testing.T) {
	args := language.GenerateArgs{
		Rel:          "src/routes/users/$userId",
		RegularFiles: []string{"$userId.tsx"},
	}
	info := buildRouteInfo(args)
	if info.RoutePattern != "/users/:userId" {
		t.Errorf("expected RoutePattern = /users/:userId, got: %q", info.RoutePattern)
	}
	want := []string{"userId"}
	assertStringSlice(t, "dynamic segments", info.DynamicSegments, want)
}

func TestBuildRouteInfo_IndexRoute(t *testing.T) {
	args := language.GenerateArgs{
		Rel:          "src/routes",
		RegularFiles: []string{"index.tsx", "about.tsx"},
	}
	info := buildRouteInfo(args)
	// The routes/ root is not a root layout (no __root.tsx listed here in
	// this variant test — it has index.tsx).
	if info.IsRootLayout {
		t.Error("expected IsRootLayout = false (no __root.tsx)")
	}
	// The routes/ dir itself maps to "/" pattern.
	if info.RoutePattern != "/" {
		t.Errorf("expected RoutePattern = /, got: %q", info.RoutePattern)
	}
}

// ---- helper ----------------------------------------------------------------

// assertStringSlice checks that got and want have the same elements in order.
func assertStringSlice(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: length mismatch got=%v want=%v", label, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s[%d]: got %q, want %q", label, i, got[i], want[i])
		}
	}
}
