package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveOpenDeleteRoundTrip(t *testing.T) {
	base := t.TempDir()
	store, err := NewFileSystem(base)
	if err != nil {
		t.Fatalf("criar armazenamento: %v", err)
	}

	name, err := store.Save("inspections", "Foto da Sala.JPG", []byte("conteúdo"))
	if err != nil {
		t.Fatalf("gravar anexo: %v", err)
	}
	if !strings.HasPrefix(name, "inspections/") {
		t.Fatalf("nome armazenado = %q, esperado prefixo do diretório", name)
	}
	if strings.Contains(name, " ") || strings.ToLower(name) != name {
		t.Fatalf("nome armazenado deveria ser saneado: %q", name)
	}
	if !strings.HasSuffix(name, ".jpg") {
		t.Fatalf("extensão perdida: %q", name)
	}

	content, err := store.Open(name)
	if err != nil {
		t.Fatalf("ler anexo: %v", err)
	}
	if string(content) != "conteúdo" {
		t.Fatalf("conteúdo = %q", string(content))
	}

	if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(name))); err != nil {
		t.Fatalf("arquivo não está no diretório base: %v", err)
	}

	if err := store.Delete(name); err != nil {
		t.Fatalf("remover anexo: %v", err)
	}
	if _, err := store.Open(name); err == nil {
		t.Fatal("anexo removido ainda pode ser lido")
	}
	// Remover de novo é idempotente: a exclusão do registro não pode falhar por
	// o arquivo já ter sumido.
	if err := store.Delete(name); err != nil {
		t.Fatalf("remover anexo ausente: %v", err)
	}
}

func TestSaveSanitizesNamesAndKeepsThemUnique(t *testing.T) {
	base := t.TempDir()
	store, err := NewFileSystem(base)
	if err != nil {
		t.Fatalf("criar armazenamento: %v", err)
	}

	first, err := store.Save("../../etc", "../../../etc/passwd", []byte("a"))
	if err != nil {
		t.Fatalf("gravar: %v", err)
	}
	second, err := store.Save("../../etc", "../../../etc/passwd", []byte("b"))
	if err != nil {
		t.Fatalf("gravar: %v", err)
	}

	for _, name := range []string{first, second} {
		if strings.Contains(name, "..") {
			t.Fatalf("nome armazenado com travessia: %q", name)
		}
		if filepath.IsAbs(name) {
			t.Fatalf("nome armazenado absoluto: %q", name)
		}
	}
	if first == second {
		t.Fatalf("dois anexos com o mesmo nome deveriam ser distintos: %q", first)
	}

	// O prefixo inválido é saneado (só alfanumérico, hífen e sublinhado): a
	// travessia desaparece e o arquivo fica dentro do diretório base.
	if strings.HasPrefix(first, "/") || strings.Contains(first, "..") {
		t.Fatalf("prefixo saneado ainda aponta para fora: %q", first)
	}
	if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(first))); err != nil {
		t.Fatalf("anexo não ficou dentro do diretório base: %v", err)
	}
}

func TestOpenRejectsTraversalAndEmptyNames(t *testing.T) {
	base := t.TempDir()
	store, err := NewFileSystem(base)
	if err != nil {
		t.Fatalf("criar armazenamento: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(base), "segredo.txt"), []byte("segredo"), 0o600); err != nil {
		t.Fatalf("preparar arquivo externo: %v", err)
	}

	for _, name := range []string{"../segredo.txt", "inspections/../../segredo.txt", "/etc/passwd", "", "   "} {
		if _, err := store.Open(name); err == nil {
			t.Errorf("abrir %q deveria ser recusado", name)
		}
		if err := store.Delete(name); err == nil {
			t.Errorf("remover %q deveria ser recusado", name)
		}
	}
}

func TestNewFileSystemCreatesDirectoryAndRejectsEmptyBase(t *testing.T) {
	base := filepath.Join(t.TempDir(), "anexos", "vistorias")
	store, err := NewFileSystem(base)
	if err != nil {
		t.Fatalf("criar armazenamento: %v", err)
	}
	if store == nil {
		t.Fatal("armazenamento nulo")
	}
	if _, err := os.Stat(base); err != nil {
		t.Fatalf("diretório base não foi criado: %v", err)
	}

	if _, err := NewFileSystem(""); err == nil {
		t.Fatal("diretório vazio deveria ser recusado")
	}
	if _, err := NewFileSystem("   "); err == nil {
		t.Fatal("diretório em branco deveria ser recusado")
	}
}
