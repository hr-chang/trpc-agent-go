//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package python provides Python source file reader implementation.
package python

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	idocument "trpc.group/trpc-go/trpc-agent-go/knowledge/document/internal/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader"
	codepython "trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader/python/internal/codeast/python"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/internal/codeast"
	itransform "trpc.group/trpc-go/trpc-agent-go/knowledge/internal/transform"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/transform"
)

var supportedExtensions = []string{".py"}

func init() {
	reader.RegisterReader(supportedExtensions, New)
}

// Reader reads Python files and extracts AST-based entities.
type Reader struct {
	chunk        bool
	transformers []transform.Transformer
	parser       *codepython.Parser
	config       Config
}

// EmbeddingTextMode controls the text embedded for Python AST documents.
type EmbeddingTextMode string

const (
	// EmbeddingTextModeLegacy preserves the existing metadata-only AST payload.
	EmbeddingTextModeLegacy EmbeddingTextMode = "legacy"
	// EmbeddingTextModeCode embeds the AST node's source code.
	EmbeddingTextModeCode EmbeddingTextMode = "code"
	// EmbeddingTextModeStructuredCode embeds stable AST fields and source code.
	EmbeddingTextModeStructuredCode EmbeddingTextMode = "structured_code"
)

// Config controls optional Python reader behavior. Zero values preserve the
// historical reader behavior.
type Config struct {
	// IncludeTestFiles includes test_*.py and *_test.py files.
	IncludeTestFiles bool
	// IncludeHiddenDirs includes Python files under hidden directories except
	// known generated/environment directories such as .git and .venv.
	IncludeHiddenDirs bool
	// StableRootModule replaces the checkout directory name in module paths.
	StableRootModule string
	// StableFilePaths reports repository-relative paths instead of checkout
	// absolute paths.
	StableFilePaths bool
	// FallbackToFile emits a whole-file document when parsing fails or yields
	// no AST nodes.
	FallbackToFile bool
	// EmbeddingTextMode selects the representation sent to the embedder.
	EmbeddingTextMode EmbeddingTextMode
}

// New creates a new Python reader with the given options.
func New(opts ...reader.Option) reader.Reader {
	return NewWithConfig(Config{}, opts...)
}

// NewWithConfig creates a Python reader with AST-specific configuration.
func NewWithConfig(pythonConfig Config, opts ...reader.Option) reader.Reader {
	config := &reader.Config{Chunk: true}
	for _, opt := range opts {
		opt(config)
	}
	if pythonConfig.EmbeddingTextMode == "" {
		pythonConfig.EmbeddingTextMode = EmbeddingTextModeLegacy
	}
	return &Reader{
		chunk:        config.Chunk,
		transformers: config.Transformers,
		parser:       codepython.NewParser(),
		config:       pythonConfig,
	}
}

// ReadFromReader reads Python content from an io.Reader and returns a list of documents.
func (r *Reader) ReadFromReader(name string, rd io.Reader) ([]*document.Document, error) {
	content, err := io.ReadAll(rd)
	if err != nil {
		return nil, err
	}
	return r.processContent(string(content), name, nil)
}

// ReadFromFile reads a Python file and returns a list of AST entity documents.
func (r *Reader) ReadFromFile(filePath string) ([]*document.Document, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".py" {
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	baseMetadata := map[string]any{
		source.MetaSource:        source.TypeFile,
		source.MetaFilePath:      filePath,
		source.MetaFileName:      filepath.Base(filePath),
		source.MetaFileExt:       filepath.Ext(filePath),
		source.MetaFileSize:      fileInfo.Size(),
		source.MetaFileMode:      fileInfo.Mode().String(),
		source.MetaModifiedAt:    fileInfo.ModTime().UTC(),
		source.MetaURI:           (&url.URL{Scheme: "file", Path: absPath}).String(),
		source.MetaSourceName:    r.Name(),
		source.MetaContentLength: utf8.RuneCountInString(string(content)),
	}

	return r.processContent(string(content), filePath, baseMetadata)
}

// ReadFromURL reads Python content from a URL and returns a list of documents.
func (r *Reader) ReadFromURL(urlStr string) ([]*document.Document, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("invalid URL scheme: %s", urlStr)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(parsedURL.String()) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read URL content: %w", err)
	}

	return r.processContent(string(content), extractFileNameFromURL(urlStr), nil)
}

// ReadFromDirectory reads a Python package directory and returns AST entity documents.
func (r *Reader) ReadFromDirectory(dirPath string) ([]*document.Document, error) {
	absDir, err := filepath.Abs(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}
	stat, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("failed to stat directory: %w", err)
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dirPath)
	}

	baseModule := filepath.Base(absDir)
	if r.config.StableRootModule != "" {
		baseModule = normalizeModuleName(r.config.StableRootModule)
	}
	baseMetadata := map[string]any{
		source.MetaSource:     source.TypeDir,
		source.MetaSourceName: r.Name(),
	}

	var allDocs []*document.Document
	var parseErrors []error
	parsedFiles := 0
	fallbackFiles := 0
	err = filepath.Walk(absDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if shouldSkipDir(info.Name(), r.config.IncludeHiddenDirs) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".py") || (!r.config.IncludeTestFiles && isTestFile(info.Name())) {
			return nil
		}
		if info.Size() == 0 {
			return nil
		}

		relPath, _ := filepath.Rel(absDir, path)
		modulePath := fileToModulePath(relPath, baseModule)

		result, parseErr := r.parser.ParseFileAt(path, modulePath)
		if parseErr != nil {
			parseErrors = append(parseErrors, fmt.Errorf("%s: %w", relPath, parseErr))
			if r.config.FallbackToFile {
				doc, fallbackErr := r.createDirectoryFallback(path, relPath, baseMetadata, "parse_error")
				if fallbackErr != nil {
					parseErrors = append(parseErrors, fmt.Errorf("%s fallback: %w", relPath, fallbackErr))
					return nil
				}
				allDocs = append(allDocs, doc)
				fallbackFiles++
			}
			return nil
		}
		parsedFiles++
		if result == nil || len(result.Nodes) == 0 {
			if r.config.FallbackToFile {
				doc, fallbackErr := r.createDirectoryFallback(path, relPath, baseMetadata, "no_nodes")
				if fallbackErr != nil {
					parseErrors = append(parseErrors, fmt.Errorf("%s fallback: %w", relPath, fallbackErr))
					return nil
				}
				allDocs = append(allDocs, doc)
				fallbackFiles++
			}
			return nil
		}

		r.stabilizeDirectoryResult(result, relPath)
		docs := r.nodesToDocuments(result, baseMetadata)
		allDocs = append(allDocs, docs...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(parseErrors) > 0 {
		if parsedFiles == 0 && fallbackFiles == 0 {
			return nil, fmt.Errorf("python reader: all %d file(s) failed to parse in %s: %w",
				len(parseErrors), dirPath, errors.Join(parseErrors...))
		}
		slog.Warn("python reader skipped files during directory read",
			"dir", dirPath,
			"failed_files", len(parseErrors),
			"parsed_files", parsedFiles,
			"fallback_files", fallbackFiles,
			"error", errors.Join(parseErrors...))
	}

	return r.applyTransformers(allDocs)
}

// Name returns the name of this reader.
func (r *Reader) Name() string {
	return "PythonReader"
}

// SupportedExtensions returns the file extensions this reader supports.
func (r *Reader) SupportedExtensions() []string {
	return supportedExtensions
}

func (r *Reader) processContent(content, name string, baseMetadata map[string]any) ([]*document.Document, error) {
	if !r.chunk {
		doc := r.createFileDocument(content, name, baseMetadata)
		return r.applyTransformers([]*document.Document{doc})
	}

	result, err := r.parser.ParseContent(name, content)
	if err != nil {
		return nil, err
	}

	docs := r.nodesToDocuments(result, baseMetadata)
	if len(docs) == 0 {
		doc := r.createFileDocumentFromInfo(content, name, baseMetadata, result.File)
		return r.applyTransformers([]*document.Document{doc})
	}

	return r.applyTransformers(docs)
}

func (r *Reader) nodesToDocuments(result *codeast.Result, baseMetadata map[string]any) []*document.Document {
	var buildEmbeddingText func(*codeast.Node) string
	switch r.config.EmbeddingTextMode {
	case EmbeddingTextModeCode:
		buildEmbeddingText = nil
	case EmbeddingTextModeStructuredCode:
		buildEmbeddingText = buildStructuredCodeEmbeddingText
	default:
		buildEmbeddingText = codepython.BuildNodeEmbeddingText
	}
	payloads := codeast.NodesToDocumentPayloads(result, codeast.NodeDocumentPayloadOptions{
		BaseMetadata:  baseMetadata,
		ScopeBasePath: repoRootFromMetadata(baseMetadata),
		FileInfo:      result.File,
		FormatType: func(entityType codeast.EntityType) string {
			return string(entityType)
		},
		BuildEmbeddingText: buildEmbeddingText,
	})
	docs := make([]*document.Document, 0, len(payloads))
	for _, payload := range payloads {
		docs = append(docs, idocument.CreateDocumentFromPayload(payload))
	}
	return docs
}

func (r *Reader) createFileDocument(content, name string, baseMetadata map[string]any) *document.Document {
	fileInfo, err := r.parser.ParseFileInfo(name, content)
	if err != nil {
		return r.createFileDocumentFromInfo(content, name, baseMetadata, nil)
	}
	return r.createFileDocumentFromInfo(content, name, baseMetadata, fileInfo)
}

func (r *Reader) createFileDocumentFromInfo(content, name string, baseMetadata map[string]any, fileInfo *codeast.FileInfo) *document.Document {
	doc := idocument.CreateDocument(content, name)
	if doc.Metadata == nil {
		doc.Metadata = make(map[string]any)
	}
	for k, v := range baseMetadata {
		doc.Metadata[k] = v
	}

	doc.Metadata["trpc_ast_type"] = "file"
	doc.Metadata["trpc_ast_name"] = name
	doc.Metadata["trpc_ast_full_name"] = name
	doc.Metadata["trpc_ast_language"] = "python"
	doc.Metadata["trpc_ast_scope"] = resolveScope(name, baseMetadata)
	doc.Metadata["trpc_ast_file_path"] = name
	if fileInfo != nil {
		if fileInfo.Package != "" {
			doc.Metadata["trpc_ast_package"] = fileInfo.Package
		}
		if len(fileInfo.Imports) > 0 {
			doc.Metadata["trpc_ast_imports"] = append([]string(nil), fileInfo.Imports...)
			doc.Metadata["trpc_ast_import_count"] = len(fileInfo.Imports)
		}
	}
	doc.Metadata[source.MetaChunkIndex] = 0
	doc.Metadata[source.MetaChunkSize] = utf8.RuneCountInString(content)
	doc.Metadata[source.MetaContentLength] = utf8.RuneCountInString(content)

	if r.config.EmbeddingTextMode == EmbeddingTextModeStructuredCode {
		doc.EmbeddingText = buildStructuredFileEmbeddingText(name, fileInfo, content)
	}
	return doc
}

func (r *Reader) createDirectoryFallback(
	filePath string,
	relPath string,
	baseMetadata map[string]any,
	reason string,
) (*document.Document, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read fallback file: %w", err)
	}
	name := filePath
	if r.config.StableFilePaths {
		name = filepath.ToSlash(relPath)
	}
	doc := r.createFileDocument(string(content), name, baseMetadata)
	doc.Metadata["trpc_ast_fallback_reason"] = reason
	return doc, nil
}

func (r *Reader) stabilizeDirectoryResult(result *codeast.Result, relPath string) {
	if result == nil || !r.config.StableFilePaths {
		return
	}
	stablePath := filepath.ToSlash(relPath)
	if result.File != nil {
		result.File.Name = stablePath
	}
	for _, node := range result.Nodes {
		if node != nil {
			node.FilePath = stablePath
		}
	}
}

type structuredCodeEmbedding struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	FullName  string `json:"full_name"`
	Package   string `json:"package,omitempty"`
	FilePath  string `json:"file_path"`
	Signature string `json:"signature,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Code      string `json:"code"`
}

func buildStructuredCodeEmbeddingText(node *codeast.Node) string {
	if node == nil {
		return ""
	}
	payload := structuredCodeEmbedding{
		Type:      string(node.Type),
		Name:      node.Name,
		FullName:  node.FullName,
		Package:   node.Package,
		FilePath:  filepath.ToSlash(node.FilePath),
		Signature: node.Signature,
		Comment:   strings.TrimSpace(node.Comment),
		Code:      node.Code,
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func buildStructuredFileEmbeddingText(name string, fileInfo *codeast.FileInfo, content string) string {
	packageName := ""
	if fileInfo != nil {
		packageName = fileInfo.Package
	}
	payload := structuredCodeEmbedding{
		Type:     "file",
		Name:     name,
		FullName: name,
		Package:  packageName,
		FilePath: filepath.ToSlash(name),
		Code:     content,
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func normalizeModuleName(name string) string {
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}

func (r *Reader) applyTransformers(docs []*document.Document) ([]*document.Document, error) {
	result, err := itransform.ApplyPreprocess(docs, r.transformers...)
	if err != nil {
		return nil, fmt.Errorf("failed to apply preprocess: %w", err)
	}
	result, err = itransform.ApplyPostprocess(result, r.transformers...)
	if err != nil {
		return nil, fmt.Errorf("failed to apply postprocess: %w", err)
	}
	return result, nil
}

func resolveScope(filePath string, baseMetadata map[string]any) string {
	if codeast.IsExamplePath(filePath, repoRootFromMetadata(baseMetadata)) {
		return string(codeast.ScopeExample)
	}
	return string(codeast.ScopeCode)
}

func repoRootFromMetadata(baseMetadata map[string]any) string {
	if baseMetadata != nil {
		if v, ok := baseMetadata[source.MetaRepoPath]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func extractFileNameFromURL(urlStr string) string {
	parts := strings.Split(urlStr, "/")
	if len(parts) == 0 {
		return "python_file.py"
	}
	fileName := parts[len(parts)-1]
	if idx := strings.Index(fileName, "?"); idx != -1 {
		fileName = fileName[:idx]
	}
	if idx := strings.Index(fileName, "#"); idx != -1 {
		fileName = fileName[:idx]
	}
	if fileName == "" {
		return "python_file.py"
	}
	return fileName
}

func shouldSkipDir(name string, includeHiddenDirs bool) bool {
	if !includeHiddenDirs && strings.HasPrefix(name, ".") {
		return true
	}
	skip := map[string]bool{
		"__pycache__": true, "node_modules": true,
		"venv": true, ".venv": true, "env": true, ".tox": true,
		"dist": true, "build": true, ".git": true,
	}
	return skip[strings.ToLower(name)]
}

func isTestFile(name string) bool {
	lower := strings.ToLower(name)
	baseName := strings.TrimSuffix(lower, ".py")
	return strings.HasPrefix(baseName, "test_") || strings.HasSuffix(baseName, "_test")
}

func fileToModulePath(relPath, baseModule string) string {
	relPath = strings.TrimSuffix(relPath, ".py")
	modulePath := strings.ReplaceAll(relPath, string(filepath.Separator), ".")
	modulePath = strings.TrimSuffix(modulePath, ".__init__")
	if modulePath == "__init__" {
		modulePath = ""
	}
	if baseModule != "" {
		if modulePath == "" {
			return baseModule
		}
		return baseModule + "." + modulePath
	}
	return modulePath
}
