package buildinfo

import (
	"os/exec"
	"regexp"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/infrastructure/platform/bootinfo"
)

// getenvFrom devolve um leitor de ambiente a partir de um mapa — o equivalente a
// rodar com apenas essas variáveis definidas.
func getenvFrom(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

var (
	fullCommitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	shortCommitPattern = regexp.MustCompile(`^[0-9a-f]{7,}$`)
	versionPattern     = regexp.MustCompile(`^1\.0\.\d+$`)
)

func TestEnvVarsMatchThePublishingWorkflow(t *testing.T) {
	want := []string{
		"ALUXEN_BUILD_COMMIT",
		"ALUXEN_BUILD_COMMIT_SHORT",
		"ALUXEN_BUILD_REF",
		"ALUXEN_BUILD_TIME",
		"ALUXEN_BUILD_IMAGE",
		"ALUXEN_BUILD_RUN_URL",
		"ALUXEN_BUILD_NUMBER",
	}
	if len(EnvVars) != len(want) {
		t.Fatalf("EnvVars = %v, esperado %v", EnvVars, want)
	}
	for i, name := range want {
		if EnvVars[i] != name {
			t.Errorf("EnvVars[%d] = %q, esperado %q", i, EnvVars[i], name)
		}
	}
}

func TestDetectFromEnvironment(t *testing.T) {
	info := detect(t.TempDir(), getenvFrom(map[string]string{
		EnvCommit:      "0123456789abcdef0123456789abcdef01234567",
		EnvCommitShort: "0123456",
		EnvRef:         "refs/tags/v1.2.3",
		EnvTime:        "2026-09-21T10:00:00Z",
		EnvImage:       "ghcr.io/passoz/aluxen:sha-0123456",
		EnvRunURL:      "https://github.com/passoz/aluxen/actions/runs/1",
		EnvBuildNumber: "42",
	}))

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Version", info.Version, "1.0.42"},
		{"PackageVersion", info.PackageVersion, Version},
		{"BuildNumber", info.BuildNumber, 42},
		{"Commit", info.Commit, "0123456789abcdef0123456789abcdef01234567"},
		{"ShortCommit", info.ShortCommit, "0123456"},
		{"Ref", info.Ref, "refs/tags/v1.2.3"},
		{"BuildTime", info.BuildTime, "2026-09-21T10:00:00Z"},
		{"Image", info.Image, "ghcr.io/passoz/aluxen:sha-0123456"},
		{"RunURL", info.RunURL, "https://github.com/passoz/aluxen/actions/runs/1"},
		{"Source", info.Source, SourceImage},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %#v, esperado %#v", check.name, check.got, check.want)
		}
	}

	if !info.StartedAt.Equal(bootinfo.StartedAt) {
		t.Errorf("StartedAt = %v, esperado o boot do processo %v", info.StartedAt, bootinfo.StartedAt)
	}
	if uptime := info.Uptime(); uptime < 0 || uptime > time.Hour {
		t.Errorf("Uptime() = %v, esperado entre 0 e 1h", uptime)
	}
}

func TestDetectWithoutEnvironmentAndWithoutGit(t *testing.T) {
	info := detect(t.TempDir(), getenvFrom(nil))

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Version", info.Version, Version},
		{"PackageVersion", info.PackageVersion, "1.0.0"},
		{"BuildNumber", info.BuildNumber, 0},
		{"Commit", info.Commit, ""},
		{"ShortCommit", info.ShortCommit, ""},
		{"Ref", info.Ref, ""},
		{"BuildTime", info.BuildTime, ""},
		{"Image", info.Image, ""},
		{"RunURL", info.RunURL, ""},
		{"Source", info.Source, SourceUnknown},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %#v, esperado %#v", check.name, check.got, check.want)
		}
	}
}

func TestDetectFallsBackToGitRepository(t *testing.T) {
	dir := initGitRepository(t)

	info := detect(dir, getenvFrom(nil))

	if got := info.Source; got != SourceLocal {
		t.Errorf("Source = %q, esperado %q", got, SourceLocal)
	}
	if !fullCommitPattern.MatchString(info.Commit) {
		t.Errorf("Commit = %q, esperado 40 caracteres hexadecimais", info.Commit)
	}
	if !shortCommitPattern.MatchString(info.ShortCommit) {
		t.Errorf("ShortCommit = %q, esperado ao menos 7 caracteres hexadecimais", info.ShortCommit)
	}
	if got := info.Ref; got != "" {
		t.Errorf("Ref = %q, esperado vazio em HEAD destacado", got)
	}
	if got := info.BuildNumber; got != 1 {
		t.Errorf("BuildNumber = %d, esperado 1 (um commit na branch)", got)
	}
	if got := info.Version; got != "1.0.1" {
		t.Errorf("Version = %q, esperado 1.0.1", got)
	}
}

func TestDetectReadsBranchNameFromGit(t *testing.T) {
	dir := initGitRepository(t)
	runGit(t, dir, "checkout", "-q", "main")

	info := detect(dir, getenvFrom(nil))
	if got := info.Ref; got != "main" {
		t.Errorf("Ref = %q, esperado main", got)
	}
}

func TestEnvironmentWinsOverGit(t *testing.T) {
	dir := initGitRepository(t)

	info := detect(dir, getenvFrom(map[string]string{EnvBuildNumber: "999"}))

	if got := info.Version; got != "1.0.999" {
		t.Errorf("Version = %q, esperado 1.0.999", got)
	}
	if got := info.Source; got != SourceLocal {
		t.Errorf("Source = %q, esperado %q (sem ALUXEN_BUILD_COMMIT)", got, SourceLocal)
	}
	if got := info.Ref; got != "" {
		t.Errorf("Ref = %q, esperado vazio", got)
	}
}

func TestGetCachesTheDetection(t *testing.T) {
	// Get lê o repositório do diretório corrente — este pacote vive dentro dele.
	first := Get()
	if first.Source != SourceLocal {
		t.Skipf("repositório git indisponível para Get(): source = %q", first.Source)
	}
	if !versionPattern.MatchString(first.Version) {
		t.Errorf("Version = %q, esperado MAJOR.MINOR.<build>", first.Version)
	}
	if !fullCommitPattern.MatchString(first.Commit) {
		t.Errorf("Commit = %q, esperado 40 caracteres hexadecimais", first.Commit)
	}
	if !shortCommitPattern.MatchString(first.ShortCommit) {
		t.Errorf("ShortCommit = %q, esperado ao menos 7 caracteres hexadecimais", first.ShortCommit)
	}
	if first.BuildNumber <= 0 {
		t.Errorf("BuildNumber = %d, esperado > 0", first.BuildNumber)
	}

	// A identificação é calculada uma única vez: mudar o ambiente depois não a altera.
	t.Setenv(EnvCommit, "0123456789abcdef0123456789abcdef01234567")
	t.Setenv(EnvBuildNumber, "9999")
	if again := Get(); again != first {
		t.Errorf("Get() recalculou a identificação: %+v vs %+v", again, first)
	}
}

// initGitRepository cria um repositório temporário com um commit, em HEAD
// destacado, para exercitar o caminho do git sem variáveis de build.
func initGitRepository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git indisponível")
	}

	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "-c", "user.name=Aluxen", "-c", "user.email=aluxen@example.com", "commit", "-q", "--allow-empty", "-m", "início")
	runGit(t, dir, "checkout", "-q", "--detach", "HEAD")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v falhou: %v (%s)", args, err, output)
	}
}
