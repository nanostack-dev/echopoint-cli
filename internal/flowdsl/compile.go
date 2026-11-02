package flowdsl

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ModulePath is the library path of a flow file, such as anchor/login.
func ModulePath(root, file string) (string, error) {
	absFile, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}
	if root == "" {
		root = filepath.Dir(absFile)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return moduleKey(absRoot, absFile), nil
}

// CompileDir compiles every flow file under root. Files with an error are
// omitted from Flows; their diagnostics are still returned.
func CompileDir(root string) (Outcome, error) {
	graphs, err := loadFlowFiles(root)
	if err != nil {
		return Outcome{}, err
	}
	if len(graphs) == 0 {
		return Outcome{}, fmt.Errorf("no flow files in %s", root)
	}
	return compileGraphs(graphs), nil
}

// CompileFile compiles one flow file. root is the library used to resolve
// module files; an empty root means the file's directory. Diagnostics of flow
// files the file does not reach are left out.
func CompileFile(file, root string) (Outcome, error) {
	absFile, err := filepath.Abs(file)
	if err != nil {
		return Outcome{}, err
	}
	info, err := os.Stat(absFile)
	if err != nil {
		return Outcome{}, err
	}
	if info.IsDir() {
		return Outcome{}, fmt.Errorf("%s is a directory; compile it as a library", file)
	}
	if root == "" {
		root = filepath.Dir(absFile)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Outcome{}, err
	}
	rel, err := filepath.Rel(absRoot, absFile)
	if err != nil || escapes(rel) {
		return Outcome{}, fmt.Errorf("%s is outside the library root %s", file, root)
	}
	if !isFlowFile(absFile) {
		return Outcome{}, fmt.Errorf("%s is not a flow file (*.flow.yaml)", file)
	}
	graphs, err := loadFlowFiles(absRoot)
	if err != nil {
		return Outcome{}, err
	}
	key := moduleKey(absRoot, absFile)
	if !hasPath(graphs, key) {
		one, readErr := readFlow(absFile, key)
		if readErr != nil {
			return Outcome{}, readErr
		}
		graphs = append(graphs, one)
	}
	return compileGraphs(graphs).filterPaths(reachable(key, graphs)), nil
}

func escapes(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func compileGraphs(graphs []*graph) Outcome {
	resolveAll(graphs)
	var out Outcome
	for _, g := range graphs {
		if g.inline {
			continue
		}
		out.Diagnostics = append(out.Diagnostics, g.collect()...)
		if g.hasError() {
			continue
		}
		request, err := g.lower()
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, Diagnostic{
				File: g.path, Severity: Error, Message: err.Error(),
			})
			continue
		}
		out.Flows = append(out.Flows, Flow{Path: g.path, Request: request})
	}
	sort.Slice(out.Diagnostics, func(i, j int) bool {
		if out.Diagnostics[i].File != out.Diagnostics[j].File {
			return out.Diagnostics[i].File < out.Diagnostics[j].File
		}
		if out.Diagnostics[i].Line != out.Diagnostics[j].Line {
			return out.Diagnostics[i].Line < out.Diagnostics[j].Line
		}
		return out.Diagnostics[i].Message < out.Diagnostics[j].Message
	})
	sort.Slice(out.Flows, func(i, j int) bool {
		return out.Flows[i].Path < out.Flows[j].Path
	})
	return out
}

func loadFlowFiles(root string) ([]*graph, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if isFlowFile(entry.Name()) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	graphs := make([]*graph, 0, len(files))
	seen := map[string]*graph{}
	for _, file := range files {
		key := moduleKey(root, file)
		one, readErr := readFlow(file, key)
		if readErr != nil {
			return nil, readErr
		}
		if prev := seen[key]; prev != nil {
			one.add(Error, 1, "module path %q is declared by more than one file", key)
			prev.add(Error, 1, "module path %q is declared by more than one file", key)
		}
		seen[key] = one
		graphs = append(graphs, one)
	}
	return graphs, nil
}

func readFlow(file, key string) (*graph, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return parseSource(key, src), nil
}

func isFlowFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".flow.yaml") || strings.HasSuffix(lower, ".flow.yml")
}

func moduleKey(root, file string) string {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		rel = file
	}
	return stripFlowSuffix(filepath.ToSlash(rel))
}

func stripFlowSuffix(value string) string {
	lower := strings.ToLower(value)
	for _, suffix := range []string{".flow.yaml", ".flow.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(lower, suffix) {
			return value[:len(value)-len(suffix)]
		}
	}
	return value
}

func hasPath(graphs []*graph, key string) bool {
	for _, g := range graphs {
		if g.path == key {
			return true
		}
	}
	return false
}

func reachable(start string, graphs []*graph) map[string]bool {
	by := map[string]*graph{}
	for _, g := range graphs {
		if by[g.path] == nil {
			by[g.path] = g
		}
	}
	keep := map[string]bool{}
	var walk func(string)
	walk = func(key string) {
		if keep[key] {
			return
		}
		g := by[key]
		if g == nil {
			return
		}
		keep[key] = true
		for _, ref := range moduleRefs(g) {
			cleaned, uuidRef, ok := modulePath(ref)
			if ok && !uuidRef {
				walk(cleaned)
			}
		}
	}
	walk(start)
	return keep
}

func moduleRefs(g *graph) []string {
	var refs []string
	for _, n := range g.nodes {
		if n.module != nil && n.module.ref != "" {
			refs = append(refs, n.module.ref)
		}
		if n.poll != nil && n.poll.body != nil {
			refs = append(refs, moduleRefs(n.poll.body)...)
		}
		if n.loop != nil && n.loop.body != nil {
			refs = append(refs, moduleRefs(n.loop.body)...)
		}
	}
	return refs
}
