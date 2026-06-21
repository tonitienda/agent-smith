package archtest

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDeclarativePluginBoundaryHasNoExecOrEgress is the structural half of the D9
// declarative-only plugin boundary guard (AS-112, plugin-trust.md §4.3). A
// third-party sub-agent ships as a manifest (data), parsed by internal/subagent's
// ParseManifest/LoadManifest and wrapped in a passive `declarative` sub-agent. For
// "data, never code" to hold, that package must have no path to arbitrary
// execution (os/exec) or network egress (net/http): nothing the parsed manifest
// flows through may grow a code-execution or exfiltration edge under a refactor.
//
// We assert it on the package's *full transitive* dependency set via `go list`, so
// the guard cannot be bypassed by reaching os/exec or net/http through an
// intermediate first-party package (the AST-import check this replaced only saw
// direct imports). The behavioral guard (TestDeclarativeBoundaryNoOp in
// internal/subagent) covers the runtime half.
func TestDeclarativePluginBoundaryHasNoExecOrEgress(t *testing.T) {
	const pkg = "github.com/tonitienda/agent-smith/internal/subagent"
	forbidden := map[string]string{
		"os/exec":  "the declarative plugin path must not reach arbitrary command execution",
		"net/http": "the declarative plugin path must not reach network egress",
	}

	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg)
	cmd.Dir = moduleRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list deps of %s: %v", pkg, err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if reason, bad := forbidden[dep]; bad {
			t.Errorf("%s transitively depends on %q: %s (AS-112, D9). See docs/design/plugin-trust.md §4.3.", pkg, dep, reason)
		}
	}
}
