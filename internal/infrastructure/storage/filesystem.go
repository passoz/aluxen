// Package storage guarda os arquivos anexados (fotos de vistoria, documentos
// enviados para leitura) fora do banco, num diretório da instância.
//
// O nome armazenado é sempre relativo ao diretório base e nunca contém caminho
// de diretório escolhido pelo cliente: isso fecha path traversal por construção.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileSystem é um AttachmentStore apoiado num diretório da instância.
type FileSystem struct {
	baseDir string
}

// NewFileSystem cria o armazenamento sobre o diretório informado, criando-o
// quando necessário.
func NewFileSystem(baseDir string) (*FileSystem, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, fmt.Errorf("diretório de armazenamento não configurado")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("criar diretório de armazenamento: %w", err)
	}
	return &FileSystem{baseDir: baseDir}, nil
}

// Save grava o conteúdo em <prefix>/<nome aleatório>-<nome saneado> e devolve o
// caminho relativo.
func (s *FileSystem) Save(prefix, filename string, data []byte) (string, error) {
	directory := sanitizeSegment(prefix)
	if directory == "" {
		directory = "attachments"
	}
	target := filepath.Join(s.baseDir, directory)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("criar diretório do anexo: %w", err)
	}

	name := randomPrefix() + "-" + sanitizeFilename(filename)
	path := filepath.Join(target, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("gravar anexo: %w", err)
	}
	return filepath.ToSlash(filepath.Join(directory, name)), nil
}

// Open devolve o conteúdo do anexo.
func (s *FileSystem) Open(name string) ([]byte, error) {
	path, err := s.resolve(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ler anexo: %w", err)
	}
	return data, nil
}

// Delete remove o anexo. Arquivo ausente não é erro: a exclusão é idempotente.
func (s *FileSystem) Delete(name string) error {
	path, err := s.resolve(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remover anexo: %w", err)
	}
	return nil
}

// resolve converte o nome relativo em caminho absoluto, recusando qualquer
// tentativa de escapar do diretório base.
func (s *FileSystem) resolve(name string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(strings.TrimSpace(name)))
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("nome de anexo vazio")
	}
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("nome de anexo inválido: %s", name)
	}
	path := filepath.Join(s.baseDir, cleaned)
	if !strings.HasPrefix(path, s.baseDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("nome de anexo fora do diretório base: %s", name)
	}
	return path, nil
}

func sanitizeSegment(value string) string {
	var b strings.Builder
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9', char == '-', char == '_':
			b.WriteRune(char)
		}
	}
	return b.String()
}

func sanitizeFilename(value string) string {
	name := filepath.Base(strings.TrimSpace(value))
	sanitized := sanitizeSegment(strings.TrimSuffix(name, filepath.Ext(name)))
	if sanitized == "" {
		sanitized = "anexo"
	}
	extension := sanitizeSegment(strings.TrimPrefix(filepath.Ext(name), "."))
	if extension == "" {
		return sanitized
	}
	return sanitized + "." + extension
}

func randomPrefix() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "anexo"
	}
	return hex.EncodeToString(buffer)
}
