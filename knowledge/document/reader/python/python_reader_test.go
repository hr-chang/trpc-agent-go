//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package python

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	docreader "trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/internal/codeast"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/transform"
)

type directoryReader interface {
	ReadFromDirectory(string) ([]*document.Document, error)
}

func newDirectoryReader(t *testing.T) directoryReader {
	t.Helper()
	r, ok := New().(directoryReader)
	if !ok {
		t.Fatal("New() reader does not support ReadFromDirectory")
	}
	return r
}

type testTransformer struct {
	preErr      error
	postErr     error
	preCalled   bool
	postCalled  bool
	metadataKey string
}

func (t *testTransformer) Preprocess(docs []*document.Document) ([]*document.Document, error) {
	t.preCalled = true
	if t.preErr != nil {
		return nil, t.preErr
	}
	for _, doc := range docs {
		if doc.Metadata == nil {
			doc.Metadata = make(map[string]any)
		}
		doc.Metadata[t.metadataKey] = "pre"
	}
	return docs, nil
}

func (t *testTransformer) Postprocess(docs []*document.Document) ([]*document.Document, error) {
	t.postCalled = true
	if t.postErr != nil {
		return nil, t.postErr
	}
	for _, doc := range docs {
		doc.Metadata[t.metadataKey] = "post"
	}
	return docs, nil
}

func (t *testTransformer) Name() string { return "test-transformer" }

var _ transform.Transformer = (*testTransformer)(nil)

func TestReaderPublicAPI(t *testing.T) {
	r := New()
	if r.Name() != "PythonReader" {
		t.Fatalf("Name() = %q, want PythonReader", r.Name())
	}
	if got := r.SupportedExtensions(); len(got) != 1 || got[0] != ".py" {
		t.Fatalf("SupportedExtensions() = %v, want [.py]", got)
	}

	docs, err := r.ReadFromReader("sample.py", strings.NewReader("class Service:\n    pass\n"))
	if err != nil {
		t.Fatalf("ReadFromReader() error = %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("ReadFromReader() returned no docs")
	}
	if docs[0].Metadata["trpc_ast_language"] != string(codeast.LanguagePython) {
		t.Fatalf("trpc_ast_language = %v, want python", docs[0].Metadata["trpc_ast_language"])
	}
}

func TestReadFromFileMetadataAndErrors(t *testing.T) {
	dir := t.TempDir()
	pyPath := filepath.Join(dir, "service.py")
	if err := os.WriteFile(pyPath, []byte("import os\nclass Service:\n    pass\n"), 0644); err != nil {
		t.Fatalf("write service.py: %v", err)
	}
	txtPath := filepath.Join(dir, "service.txt")
	if err := os.WriteFile(txtPath, []byte("not python"), 0644); err != nil {
		t.Fatalf("write service.txt: %v", err)
	}

	r := New()
	docs, err := r.ReadFromFile(pyPath)
	if err != nil {
		t.Fatalf("ReadFromFile() error = %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("ReadFromFile() returned no docs")
	}
	metadata := docs[0].Metadata
	if metadata[source.MetaSource] != source.TypeFile {
		t.Fatalf("source metadata = %v, want %s", metadata[source.MetaSource], source.TypeFile)
	}
	if metadata[source.MetaFilePath] != pyPath {
		t.Fatalf("file path metadata = %v, want %s", metadata[source.MetaFilePath], pyPath)
	}
	if !strings.HasPrefix(metadata[source.MetaURI].(string), "file://") {
		t.Fatalf("uri metadata = %v, want file URI", metadata[source.MetaURI])
	}

	if _, err := r.ReadFromFile(txtPath); err == nil || !strings.Contains(err.Error(), "unsupported file extension") {
		t.Fatalf("ReadFromFile(.txt) error = %v, want unsupported extension", err)
	}
	if _, err := r.ReadFromFile(filepath.Join(dir, "missing.py")); err == nil || !strings.Contains(err.Error(), "failed to read file") {
		t.Fatalf("ReadFromFile(missing) error = %v, want read error", err)
	}
}

func TestReadFromURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/error.py" {
			http.Error(w, "bad", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("def run():\n    return 1\n"))
	}))
	defer server.Close()

	r := New()
	docs, err := r.ReadFromURL(server.URL + "/pkg/mod.py?download=1#frag")
	if err != nil {
		t.Fatalf("ReadFromURL() error = %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("ReadFromURL() returned no docs")
	}
	if docs[0].Metadata["trpc_ast_file_path"] != "mod.py" {
		t.Fatalf("trpc_ast_file_path = %v, want mod.py", docs[0].Metadata["trpc_ast_file_path"])
	}

	if _, err := r.ReadFromURL("ftp://example.com/mod.py"); err == nil || !strings.Contains(err.Error(), "invalid URL scheme") {
		t.Fatalf("ReadFromURL(ftp) error = %v, want scheme error", err)
	}
	if _, err := r.ReadFromURL(server.URL + "/error.py"); err == nil || !strings.Contains(err.Error(), "HTTP error: 500") {
		t.Fatalf("ReadFromURL(500) error = %v, want HTTP error", err)
	}
}

func TestReadWithoutChunkCreatesFileDocument(t *testing.T) {
	r := New(docreader.WithChunk(false))
	docs, err := r.ReadFromReader("pkg/sample.py", strings.NewReader("import os\nvalue = 1\n"))
	if err != nil {
		t.Fatalf("ReadFromReader() error = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("ReadFromReader() returned %d docs, want 1", len(docs))
	}
	metadata := docs[0].Metadata
	if metadata["trpc_ast_type"] != "file" {
		t.Fatalf("trpc_ast_type = %v, want file", metadata["trpc_ast_type"])
	}
	if metadata["trpc_ast_package"] != "pkg.sample" {
		t.Fatalf("trpc_ast_package = %v, want pkg.sample", metadata["trpc_ast_package"])
	}
	if metadata["trpc_ast_import_count"] != 1 {
		t.Fatalf("trpc_ast_import_count = %v, want 1", metadata["trpc_ast_import_count"])
	}
}

func TestReadWithoutChunkFallsBackWhenFileInfoFails(t *testing.T) {
	r := New(docreader.WithChunk(false))
	docs, err := r.ReadFromReader("broken.py", strings.NewReader("def broken(:\n"))
	if err != nil {
		t.Fatalf("ReadFromReader() error = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("ReadFromReader() returned %d docs, want fallback file doc", len(docs))
	}
	if docs[0].Metadata["trpc_ast_package"] != nil {
		t.Fatalf("trpc_ast_package = %v, want no package when file info fails", docs[0].Metadata["trpc_ast_package"])
	}
}

func TestReadAppliesTransformers(t *testing.T) {
	transformer := &testTransformer{metadataKey: "stage"}
	r := New(docreader.WithTransformers(transformer))
	docs, err := r.ReadFromReader("sample.py", strings.NewReader("class Service:\n    pass\n"))
	if err != nil {
		t.Fatalf("ReadFromReader() error = %v", err)
	}
	if !transformer.preCalled || !transformer.postCalled {
		t.Fatalf("transformer calls pre=%v post=%v, want both", transformer.preCalled, transformer.postCalled)
	}
	if docs[0].Metadata["stage"] != "post" {
		t.Fatalf("stage metadata = %v, want post", docs[0].Metadata["stage"])
	}
}

func TestReadTransformerErrors(t *testing.T) {
	preErr := errors.New("pre failed")
	r := New(docreader.WithTransformers(&testTransformer{preErr: preErr}))
	if _, err := r.ReadFromReader("sample.py", strings.NewReader("class Service:\n    pass\n")); err == nil ||
		!strings.Contains(err.Error(), "failed to apply preprocess") {
		t.Fatalf("ReadFromReader() preprocess error = %v, want wrapped preprocess error", err)
	}

	postErr := errors.New("post failed")
	r = New(docreader.WithTransformers(&testTransformer{postErr: postErr}))
	if _, err := r.ReadFromReader("sample.py", strings.NewReader("class Service:\n    pass\n")); err == nil ||
		!strings.Contains(err.Error(), "failed to apply postprocess") {
		t.Fatalf("ReadFromReader() postprocess error = %v, want wrapped postprocess error", err)
	}
}

func TestResolveScope(t *testing.T) {
	root := t.TempDir()
	examplePath := filepath.Join(root, "examples", "demo.py")
	if got := resolveScope(examplePath, map[string]any{source.MetaRepoPath: root}); got != string(codeast.ScopeExample) {
		t.Fatalf("resolveScope(example) = %q, want example", got)
	}
	if got := resolveScope(filepath.Join(root, "pkg", "demo.py"), map[string]any{source.MetaRepoPath: 123}); got != string(codeast.ScopeCode) {
		t.Fatalf("resolveScope(non-string repo root) = %q, want code", got)
	}
}

func TestReadFromDirectoryContinuesWhenSomeFilesFail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "good.py"), []byte("class Good:\n    pass\n"), 0644); err != nil {
		t.Fatalf("write good.py: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.py"), []byte("def broken(:\n"), 0644); err != nil {
		t.Fatalf("write bad.py: %v", err)
	}

	r := newDirectoryReader(t)
	docs, err := r.ReadFromDirectory(dir)
	if err != nil {
		t.Fatalf("ReadFromDirectory() error = %v, want nil for partial failure", err)
	}
	if len(docs) == 0 {
		t.Fatal("ReadFromDirectory() returned no docs from successfully parsed file")
	}
}

func TestReadFromDirectoryReturnsErrorWhenAllFilesFail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.py"), []byte("def broken(:\n"), 0644); err != nil {
		t.Fatalf("write bad.py: %v", err)
	}

	r := newDirectoryReader(t)
	_, err := r.ReadFromDirectory(dir)
	if err == nil {
		t.Fatal("ReadFromDirectory() error = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "all 1 file(s) failed") || !strings.Contains(err.Error(), "bad.py") {
		t.Fatalf("ReadFromDirectory() error = %v, want all-files-failed context with file name", err)
	}
}

func TestReadFromDirectoryConfiguredFallbackAndStableEmbedding(t *testing.T) {
	makeTree := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		files := map[string]string{
			"pkg/service.py":      "class Service:\n    def run(self):\n        return 1\n",
			"pkg/test_service.py": "def test_run():\n    assert True\n",
			"pkg/broken.py":       "def broken(:\n",
			"pkg/constants.py":    "# constants only\n",
			".ci/check.py":        "def check():\n    return True\n",
		}
		for name, content := range files {
			path := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatalf("mkdir %s: %v", name, err)
			}
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		return dir
	}
	read := func(t *testing.T, dir string) []*document.Document {
		t.Helper()
		r, ok := NewWithConfig(Config{
			IncludeTestFiles:  true,
			IncludeHiddenDirs: true,
			StableRootModule:  "owner/project-name",
			StableFilePaths:   true,
			FallbackToFile:    true,
			EmbeddingTextMode: EmbeddingTextModeStructuredCode,
		}).(directoryReader)
		if !ok {
			t.Fatal("configured reader does not support ReadFromDirectory")
		}
		docs, err := r.ReadFromDirectory(dir)
		if err != nil {
			t.Fatalf("ReadFromDirectory() error = %v", err)
		}
		return docs
	}

	firstRoot := makeTree(t)
	secondRoot := makeTree(t)
	first := read(t, firstRoot)
	second := read(t, secondRoot)

	summarize := func(docs []*document.Document) []string {
		var summary []string
		files := make(map[string]bool)
		fallbacks := make(map[string]string)
		for _, doc := range docs {
			path, _ := doc.Metadata["trpc_ast_file_path"].(string)
			files[path] = true
			if reason, ok := doc.Metadata["trpc_ast_fallback_reason"].(string); ok {
				fallbacks[path] = reason
			}
			if strings.Contains(doc.EmbeddingText, firstRoot) ||
				strings.Contains(doc.EmbeddingText, secondRoot) {
				t.Fatalf("embedding text leaks checkout path: %s", doc.EmbeddingText)
			}
			summary = append(summary, path+"\x00"+doc.Name+"\x00"+doc.EmbeddingText)
		}
		for _, want := range []string{
			"pkg/service.py",
			"pkg/test_service.py",
			"pkg/broken.py",
			"pkg/constants.py",
			".ci/check.py",
		} {
			if !files[want] {
				t.Errorf("indexed files = %v, missing %s", files, want)
			}
		}
		if fallbacks["pkg/broken.py"] != "parse_error" {
			t.Errorf("broken.py fallback = %q, want parse_error", fallbacks["pkg/broken.py"])
		}
		if fallbacks["pkg/constants.py"] != "no_nodes" {
			t.Errorf("constants.py fallback = %q, want no_nodes", fallbacks["pkg/constants.py"])
		}
		slices.Sort(summary)
		return summary
	}

	firstSummary := summarize(first)
	secondSummary := summarize(second)
	if !slices.Equal(firstSummary, secondSummary) {
		t.Fatalf("stable representations differ across checkout roots:\nfirst=%v\nsecond=%v",
			firstSummary, secondSummary)
	}
}

func TestEmbeddingTextModes(t *testing.T) {
	const content = "def run():\n    return 1\n"

	codeDocs, err := NewWithConfig(Config{
		EmbeddingTextMode: EmbeddingTextModeCode,
	}).ReadFromReader("sample.py", strings.NewReader(content))
	if err != nil {
		t.Fatalf("code mode ReadFromReader() error = %v", err)
	}
	for _, doc := range codeDocs {
		if doc.EmbeddingText != "" {
			t.Fatalf("code mode EmbeddingText = %q, want empty to embed Content", doc.EmbeddingText)
		}
	}

	structuredDocs, err := NewWithConfig(Config{
		EmbeddingTextMode: EmbeddingTextModeStructuredCode,
	}).ReadFromReader("sample.py", strings.NewReader(content))
	if err != nil {
		t.Fatalf("structured mode ReadFromReader() error = %v", err)
	}
	if len(structuredDocs) == 0 {
		t.Fatal("structured mode returned no documents")
	}
	if !strings.Contains(structuredDocs[0].EmbeddingText, `"code":`) ||
		!strings.Contains(structuredDocs[0].EmbeddingText, `return 1`) {
		t.Fatalf("structured EmbeddingText = %q, want AST fields plus code",
			structuredDocs[0].EmbeddingText)
	}
}

func TestReadFromDirectoryFallbackWhenAllFilesFail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.py"), []byte("def broken(:\n"), 0644); err != nil {
		t.Fatalf("write bad.py: %v", err)
	}
	r, ok := NewWithConfig(Config{
		StableRootModule: "repo",
		StableFilePaths:  true,
		FallbackToFile:   true,
	}).(directoryReader)
	if !ok {
		t.Fatal("configured reader does not support ReadFromDirectory")
	}
	docs, err := r.ReadFromDirectory(dir)
	if err != nil {
		t.Fatalf("ReadFromDirectory() error = %v, want fallback success", err)
	}
	if len(docs) != 1 || docs[0].Metadata["trpc_ast_fallback_reason"] != "parse_error" {
		t.Fatalf("fallback docs = %#v, want one parse_error document", docs)
	}
}

func TestFileToModulePathInitFiles(t *testing.T) {
	tests := []struct {
		relPath    string
		baseModule string
		want       string
	}{
		{relPath: "__init__.py", baseModule: "pkg", want: "pkg"},
		{relPath: filepath.Join("sub", "__init__.py"), baseModule: "pkg", want: "pkg.sub"},
		{relPath: filepath.Join("sub", "mod.py"), baseModule: "pkg", want: "pkg.sub.mod"},
		{relPath: "__init__.py", want: ""},
	}
	for _, tt := range tests {
		if got := fileToModulePath(tt.relPath, tt.baseModule); got != tt.want {
			t.Errorf("fileToModulePath(%q, %q) = %q, want %q", tt.relPath, tt.baseModule, got, tt.want)
		}
	}
}
