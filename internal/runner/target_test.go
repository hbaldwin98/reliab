package runner

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// Source for every test in this file: knowledge/decisions/profile-v2.md,
// "Targets": a project file, folder, file or file#Function resolves to
// files, line ranges, owning projects and their tests; change and pr
// default to what changed since the base.

func dotnetRepo(t *testing.T) string {
	t.Helper()
	dir := testrepo.New(t, "")
	testrepo.Write(t, dir, "src/Api/Api.csproj", "<Project/>")
	testrepo.Write(t, dir, "src/Api/Data/Repo.cs", "class Repo {\n  public int Get() {\n    return 1;\n  }\n}\n")
	testrepo.Write(t, dir, "src/Api/Users.cs", "class Users {\n}\n")
	testrepo.Write(t, dir, "tests/Api.Tests/Api.Tests.csproj", `<Project><ItemGroup><ProjectReference Include="..\..\src\Api\Api.csproj" /></ItemGroup></Project>`)
	testrepo.Write(t, dir, "tests/Other.Tests/Other.Tests.csproj", `<Project><ItemGroup><ProjectReference Include="..\..\src\Web\Web.csproj" /></ItemGroup></Project>`)
	testrepo.Write(t, dir, "web/package.json", "{}")
	testrepo.Write(t, dir, "web/lib/sum.ts", "export function sum() {}\n")
	testrepo.Write(t, dir, "web/lib/sum.test.ts", "test()\n")
	testrepo.Write(t, dir, "go/pkg/x.go", "package pkg\n\nfunc X() {\n}\n")
	testrepo.Write(t, dir, "go/pkg/x_test.go", "package pkg\n")
	testrepo.Git(t, dir, "add", "-A")
	testrepo.Git(t, dir, "commit", "-qm", "code")
	return dir
}

func TestResolveProjectFile(t *testing.T) {
	dir := dotnetRepo(t)
	tg, err := Resolve(dir, []string{filepath.Join(dir, "src/Api/Api.csproj")})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tg.Files, ",") != "src/Api/Api.csproj,src/Api/Data/Repo.cs,src/Api/Users.cs" {
		t.Fatalf("files = %v", tg.Files)
	}
	if strings.Join(tg.Projects, ",") != "src/Api/Api.csproj" {
		t.Fatalf("projects = %v", tg.Projects)
	}
	if strings.Join(tg.Tests, ",") != "tests/Api.Tests/Api.Tests.csproj" {
		t.Fatalf("tests = %v: only the test project that references Api.csproj", tg.Tests)
	}
}

func TestResolveFolderAndFunction(t *testing.T) {
	dir := dotnetRepo(t)
	tg, err := Resolve(dir, []string{filepath.Join(dir, "src/Api/Data/Repo.cs") + "#Get"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tg.Files, ",") != "src/Api/Data/Repo.cs" || tg.Ranges["src/Api/Data/Repo.cs"][0] != [2]int{2, 4} {
		t.Fatalf("files=%v ranges=%v", tg.Files, tg.Ranges)
	}
	if strings.Join(tg.Projects, ",") != "src/Api/Api.csproj" {
		t.Fatalf("projects = %v: the nearest project above the file", tg.Projects)
	}

	tg, err = Resolve(dir, []string{filepath.Join(dir, "web/lib")})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tg.Projects, ",") != "web" || strings.Join(tg.Tests, ",") != "web/lib/sum.test.ts" {
		t.Fatalf("projects=%v tests=%v", tg.Projects, tg.Tests)
	}
}

func TestResolveGoPackage(t *testing.T) {
	dir := dotnetRepo(t)
	tg, err := Resolve(dir, []string{filepath.Join(dir, "go/pkg/x.go") + "#X"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tg.Projects, ",") != "./go/pkg" || strings.Join(tg.Tests, ",") != "./go/pkg" {
		t.Fatalf("projects=%v tests=%v: a Go file's project and test unit is its package", tg.Projects, tg.Tests)
	}
}

func TestResolveErrors(t *testing.T) {
	dir := dotnetRepo(t)
	for spec, want := range map[string]string{
		filepath.Join(dir, "nope.cs"):              "nope.cs",
		filepath.Join(dir, "src/Api") + "#Get":     "names a file, not a folder",
		filepath.Join(dir, "src/Api/Users.cs#Get"): "no function Get",
		filepath.Join(t.TempDir(), "x"):            "not inside the repository",
	} {
		_, err := Resolve(dir, []string{spec})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", spec, err, want)
		}
	}
}

func TestResolveChanged(t *testing.T) {
	dir := dotnetRepo(t)
	testrepo.Write(t, dir, "src/Api/Users.cs", "class Users {\n  int n;\n}\n")
	testrepo.Write(t, dir, "src/Api/New.cs", "class New {}\n")
	testrepo.Git(t, dir, "rm", "-q", "web/lib/sum.ts")
	tg, err := ResolveChanged(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tg.Files, ",") != "src/Api/New.cs,src/Api/Users.cs" {
		t.Fatalf("files = %v: changed and untracked, not deleted", tg.Files)
	}
	if r := tg.Ranges["src/Api/Users.cs"]; len(r) != 1 || r[0] != [2]int{2, 2} {
		t.Fatalf("Users.cs ranges = %v: only the changed line", r)
	}
	if !tg.Contains("src/Api/Users.cs", 2) || tg.Contains("src/Api/Users.cs", 1) {
		t.Fatal("Contains must follow the changed lines")
	}
}
