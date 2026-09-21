// Package buildinfo identifica o build em execução.
//
// Espelha `src/utils/build-info.ts`: na imagem publicada os valores chegam por
// variáveis de ambiente gravadas pelo workflow de publicação; em execução local
// caem para o repositório git, quando disponível. A versão exibida é
// MAJOR.MINOR da versão declarada com o número do build como patch
// (ex.: `1.0.42`), de modo que ela identifica exatamente o que está rodando.
package buildinfo

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/passoz/aluxen/internal/infrastructure/platform/bootinfo"
)

// Version é a versão declarada do app (MAJOR.MINOR do `package.json`); o patch
// exibido em Info.Version é o número do build.
const Version = "1.0.0"

// Nomes das variáveis gravadas na imagem pelo workflow de publicação.
const (
	EnvCommit      = "ALUXEN_BUILD_COMMIT"
	EnvCommitShort = "ALUXEN_BUILD_COMMIT_SHORT"
	EnvRef         = "ALUXEN_BUILD_REF"
	EnvTime        = "ALUXEN_BUILD_TIME"
	EnvImage       = "ALUXEN_BUILD_IMAGE"
	EnvRunURL      = "ALUXEN_BUILD_RUN_URL"
	EnvBuildNumber = "ALUXEN_BUILD_NUMBER"
)

// EnvVars lista as variáveis de build na ordem em que o app as lê.
var EnvVars = [...]string{EnvCommit, EnvCommitShort, EnvRef, EnvTime, EnvImage, EnvRunURL, EnvBuildNumber}

// Origem do build.
const (
	// SourceImage indica build vindo do pipeline de publicação.
	SourceImage = "image"
	// SourceLocal indica execução de desenvolvimento dentro de um repositório git.
	SourceLocal = "local"
	// SourceUnknown indica ausência de metadados (imagem publicada antes da
	// injeção das variáveis de build).
	SourceUnknown = "unknown"
)

// Info descreve o build em execução. Campos não identificados ficam vazios (ou
// zero, no caso de BuildNumber).
type Info struct {
	// Version é a versão efetiva: MAJOR.MINOR.<número do build>.
	Version string `json:"version"`
	// PackageVersion é a versão declarada (base MAJOR.MINOR).
	PackageVersion string `json:"packageVersion"`
	// BuildNumber é o número sequencial do build (commits da branch ou número da
	// execução no CI); zero quando indeterminado.
	BuildNumber int `json:"buildNumber"`
	// Commit é o commit completo (40 caracteres) quando identificado.
	Commit string `json:"commit"`
	// ShortCommit é o commit curto — o mesmo sufixo usado na tag `sha-<commit>` da imagem.
	ShortCommit string `json:"shortCommit"`
	// Ref é a branch ou tag que originou o build.
	Ref string `json:"ref"`
	// BuildTime é o momento em que a imagem foi construída (ISO 8601, UTC).
	BuildTime string `json:"buildTime"`
	// Image é a referência exata da imagem no GHCR, quando publicada pelo workflow.
	Image string `json:"image"`
	// RunURL é o link da execução do GitHub Actions que gerou a imagem.
	RunURL string `json:"runUrl"`
	// Source é `image`, `local` ou `unknown`.
	Source string `json:"source"`
	// StartedAt é o instante em que o processo subiu.
	StartedAt time.Time `json:"startedAt"`
}

// Uptime é o tempo decorrido desde o boot do processo.
func (i Info) Uptime() time.Duration { return time.Since(i.StartedAt) }

var (
	once   sync.Once
	cached Info
)

// Get devolve a identificação do build do processo em execução, calculada uma
// única vez (a leitura do git não se repete a cada chamada).
func Get() Info {
	once.Do(func() { cached = detect(".", os.Getenv) })
	return cached
}

// detect monta a identificação lendo o ambiente e, quando falta informação, o
// repositório git de dir.
func detect(dir string, getenv func(string) string) Info {
	envCommit := readEnv(getenv, EnvCommit)
	gitCommit, _ := gitValue(dir, "rev-parse", "HEAD")

	buildNumber := envBuildNumber(getenv)
	if buildNumber == 0 {
		buildNumber = gitBuildNumber(dir)
	}

	major, minor := majorMinor(Version)
	version := Version
	if buildNumber > 0 {
		version = fmt.Sprintf("%s.%s.%d", major, minor, buildNumber)
	}

	commit := envCommit
	if commit == "" {
		commit = gitCommit
	}
	shortCommit := readEnv(getenv, EnvCommitShort)
	if shortCommit == "" {
		shortCommit, _ = gitValue(dir, "rev-parse", "--short", "HEAD")
	}
	ref := readEnv(getenv, EnvRef)
	if ref == "" {
		ref, _ = gitValue(dir, "rev-parse", "--abbrev-ref", "HEAD")
	}

	source := SourceUnknown
	switch {
	case envCommit != "":
		source = SourceImage
	case gitCommit != "":
		source = SourceLocal
	}

	return Info{
		Version:        version,
		PackageVersion: Version,
		BuildNumber:    buildNumber,
		Commit:         commit,
		ShortCommit:    shortCommit,
		Ref:            ref,
		BuildTime:      readEnv(getenv, EnvTime),
		Image:          readEnv(getenv, EnvImage),
		RunURL:         readEnv(getenv, EnvRunURL),
		Source:         source,
		StartedAt:      bootinfo.StartedAt,
	}
}

// readEnv devolve a variável trimada, ou a string vazia quando ausente.
func readEnv(getenv func(string) string, name string) string {
	return strings.TrimSpace(getenv(name))
}

// envBuildNumber lê o número do build informado pelo pipeline.
func envBuildNumber(getenv func(string) string) int {
	raw := readEnv(getenv, EnvBuildNumber)
	number, err := strconv.Atoi(raw)
	if err != nil || number <= 0 {
		return 0
	}
	return number
}

// gitBuildNumber conta os commits alcançáveis da branch corrente — em
// desenvolvimento é o número sempre crescente do build.
func gitBuildNumber(dir string) int {
	output, ok := gitValue(dir, "rev-list", "--count", "HEAD")
	if !ok {
		return 0
	}
	number, err := strconv.Atoi(output)
	if err != nil || number <= 0 {
		return 0
	}
	return number
}

// majorMinor extrai MAJOR e MINOR da versão declarada, com os mesmos padrões do
// app (`1` e `0`) quando ela é inválida.
func majorMinor(version string) (major, minor string) {
	parts := strings.Split(version, ".")
	major, minor = "1", "0"
	if len(parts) > 0 && parts[0] != "" {
		major = parts[0]
	}
	if len(parts) > 1 && parts[1] != "" {
		minor = parts[1]
	}
	return major, minor
}

// gitValue executa um comando git de leitura em dir e devolve a saída; o
// segundo retorno é falso quando o git não está disponível, quando dir não é um
// repositório ou quando a saída é `HEAD` (estado destacado, sem branch).
func gitValue(dir string, args ...string) (string, bool) {
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		return "", false
	}
	value := strings.TrimSpace(string(output))
	if value == "" || value == "HEAD" {
		return "", false
	}
	return value, true
}
