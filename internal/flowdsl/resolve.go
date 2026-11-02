package flowdsl

import (
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// refPattern matches {{node.output}}. A {{name}} with no dot is an initial input.
var refPattern = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_-]+)\.([A-Za-z0-9_$.\[\]-]+)\s*\}\}`)

var bindingPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)

const (
	stateFresh = 0
	stateVisit = 1
	stateDone  = 2
)

type resolver struct {
	by    map[string]*graph
	state map[string]int
	stack []string
}

func resolveAll(graphs []*graph) {
	r := &resolver{by: map[string]*graph{}, state: map[string]int{}}
	for _, g := range graphs {
		if g.inline || r.by[g.path] != nil {
			continue
		}
		r.by[g.path] = g
	}
	for _, g := range graphs {
		if !g.inline {
			r.visit(g)
		}
	}
}

func (r *resolver) visit(g *graph) {
	if g.inline {
		r.finish(g)
		return
	}
	switch r.state[g.path] {
	case stateDone, stateVisit:
		return
	}
	r.state[g.path] = stateVisit
	r.stack = append(r.stack, g.path)
	r.finish(g)
	r.stack = r.stack[:len(r.stack)-1]
	r.state[g.path] = stateDone
}

func (r *resolver) finish(g *graph) {
	g.warnFolder()
	for _, n := range g.nodes {
		r.bindModule(g, n)
		r.visitBody(n)
	}
	g.lintEdges()
	g.lintRefs()
	g.lintFailure()
	g.fillExports()
}

func (r *resolver) visitBody(n *dslNode) {
	if n.poll != nil && n.poll.body != nil {
		r.visit(n.poll.body)
	}
	if n.loop != nil && n.loop.body != nil {
		r.visit(n.loop.body)
	}
}

func (g *graph) warnFolder() {
	if g.inline || g.folder == "" || isFlowID(g.folder) {
		return
	}
	g.add(
		Warning,
		g.folderLine,
		"folder %q is a path, and folder_id needs the folder's id, so it was left unset",
		g.folder,
	)
}

func (r *resolver) bindModule(g *graph, n *dslNode) {
	if n.kind != kindModule || n.module == nil {
		return
	}
	ref, uuidRef, ok := modulePath(n.module.ref)
	if !ok {
		g.add(Error, n.module.refLine, "node %q module %q is outside the library", n.id, n.module.ref)
		return
	}
	if uuidRef {
		n.module.flowID = ref
		checkBindingShape(g, n)
		return
	}
	if r.state[ref] == stateVisit {
		g.add(
			Error,
			n.module.refLine,
			"module cycle: %s",
			strings.Join(append(append([]string{}, r.stack...), ref), " -> "),
		)
		return
	}
	dep := r.by[ref]
	if dep == nil {
		g.add(
			Error,
			n.module.refLine,
			"node %q module %q does not match a flow file under the root",
			n.id,
			n.module.ref,
		)
		return
	}
	r.visit(dep)
	if strings.TrimSpace(dep.id) == "" {
		g.add(
			Error,
			n.module.refLine,
			"node %q module %q has no id; create the module, then set id to its flow id",
			n.id,
			ref,
		)
		return
	}
	if !isFlowID(dep.id) {
		g.add(Error, n.module.refLine, "node %q module %q id %q is not a flow id", n.id, ref, dep.id)
		return
	}
	n.module.flowID = dep.id
	checkInputs(g, n, dep)
	r.fillBindings(g, n, dep)
}

func (r *resolver) fillBindings(g *graph, n *dslNode, dep *graph) {
	if n.module.explicit {
		known := map[string]bool{}
		for _, binding := range dep.exports {
			known[binding] = true
		}
		checkBindingShape(g, n)
		for _, out := range n.outputs {
			if !bindingPattern.MatchString(out.binding) || known[out.binding] {
				continue
			}
			g.add(
				Error,
				out.line,
				"node %q output %q binds %q, which %s does not declare",
				n.id,
				out.name,
				out.binding,
				dep.path,
			)
		}
		return
	}
	names := make([]string, 0, len(dep.exports))
	for name := range dep.exports {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		n.outputs = append(n.outputs, outDecl{name: name, binding: dep.exports[name]})
	}
}

func checkInputs(g *graph, n *dslNode, dep *graph) {
	if len(dep.inputs) == 0 {
		return
	}
	allowed := map[string]bool{}
	for _, name := range dep.inputs {
		allowed[name] = true
	}
	given := map[string]bool{}
	for _, item := range n.module.inputs {
		given[item.key] = true
		if !allowed[item.key] {
			g.add(Error, n.module.refLine, "node %q module %q has no input %q", n.id, dep.path, item.key)
		}
	}
	for _, name := range dep.inputs {
		if !given[name] {
			g.add(Error, n.module.refLine, "node %q module %q needs input %q", n.id, dep.path, name)
		}
	}
}

func checkBindingShape(g *graph, n *dslNode) {
	for _, out := range n.outputs {
		if out.binding == "" || bindingPattern.MatchString(out.binding) {
			continue
		}
		g.add(Error, out.line, "node %q output %q must be a child output like request.accessToken", n.id, out.name)
	}
}

// modulePath cleans a module reference. The bool is true when ref is a flow id.
func modulePath(ref string) (string, bool, bool) {
	trimmed := strings.TrimSpace(ref)
	if _, err := uuid.Parse(trimmed); err == nil {
		return trimmed, true, true
	}
	cleaned := strings.TrimPrefix(trimmed, "./")
	cleaned = path.Clean(strings.ReplaceAll(cleaned, "\\", "/"))
	cleaned = stripFlowSuffix(cleaned)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false, false
	}
	return cleaned, false, true
}

func (g *graph) lintEdges() {
	ids := nodeSet(g)
	for _, n := range g.nodes {
		for _, target := range n.next {
			g.knownNode(n, "next", target, ids)
		}
		for _, target := range n.fail {
			g.knownNode(n, "on_failure", target, ids)
		}
		if n.branch == nil {
			continue
		}
		for _, item := range n.branch.cases {
			g.knownNode(n, "branch target", item.target, ids)
		}
		if n.branch.hasDefault {
			g.knownNode(n, "default", n.branch.defaultTarget, ids)
		}
	}
}

func (g *graph) knownNode(n *dslNode, what, target string, ids map[string]bool) {
	if target == "" || ids[target] {
		return
	}
	g.add(Error, n.line, "node %q %s %q does not exist", n.id, what, target)
}

func (g *graph) lintFailure() {
	byID := map[string]*dslNode{}
	for _, n := range g.nodes {
		byID[n.id] = n
	}
	for _, n := range g.nodes {
		for _, target := range n.fail {
			dest := byID[target]
			if dest == nil || dest.runWhen == "always" {
				continue
			}
			g.add(
				Warning,
				n.line,
				"node %q on_failure goes to %q with run_when %s; set run_when: always",
				n.id,
				target,
				dest.runWhen,
			)
		}
	}
}

func (g *graph) lintRefs() {
	ids := nodeSet(g)
	declared := map[string]map[string]bool{}
	for _, n := range g.nodes {
		declared[n.id] = declaredOutputs(n)
	}
	injected := map[string]bool{}
	for _, name := range g.injected {
		injected[name] = true
	}
	for _, n := range g.nodes {
		for _, text := range nodeTexts(n) {
			g.lintText(n, text, ids, declared, injected)
		}
	}
}

func (g *graph) lintText(
	n *dslNode,
	text string,
	ids map[string]bool,
	declared map[string]map[string]bool,
	injected map[string]bool,
) {
	for _, match := range refPattern.FindAllStringSubmatch(text, -1) {
		source, output := match[1], match[2]
		if injected[source] && !ids[source] {
			continue
		}
		if source == "webhook" && !ids["webhook"] && (output == "url" || output == "requests_url") {
			continue
		}
		if !ids[source] {
			g.missingRef(n, source, output)
			continue
		}
		if !declaresOutput(declared[source], output) {
			g.add(
				Error,
				n.line,
				"node %q references {{%s.%s}} but node %q does not declare output %q",
				n.id,
				source,
				output,
				source,
				output,
			)
		}
	}
}

func (g *graph) missingRef(n *dslNode, source, output string) {
	if g.inline {
		g.add(
			Error,
			n.line,
			"node %q references {{%s.%s}} but a poll or loop body only sees its own nodes and the flow's initial inputs",
			n.id,
			source,
			output,
		)
		return
	}
	g.add(Error, n.line, "node %q references {{%s.%s}} but node %q does not exist", n.id, source, output, source)
}

func nodeSet(g *graph) map[string]bool {
	ids := map[string]bool{}
	for _, n := range g.nodes {
		ids[n.id] = true
	}
	return ids
}

func declaredOutputs(n *dslNode) map[string]bool {
	out := map[string]bool{}
	switch n.kind {
	case kindSet:
		for _, item := range n.vars {
			out[item.key] = true
		}
	case kindBranch:
		out["matched"] = true
		out["matchedIndex"] = true
	case kindPoll:
		out["attempts"] = true
		out["result"] = true
	case kindLoop:
		out["results"] = true
		out["count"] = true
	case kindDelay:
		return out
	case kindSSE:
		// The runner always publishes these, whether or not the file lists them.
		out["events"] = true
		out["count"] = true
		out["last"] = true
		addDeclared(out, n.outputs)
	case kindWebhook:
		for _, name := range []string{"id", "method", "headers", "query", "body", "received_at"} {
			out[name] = true
		}
		addDeclared(out, n.outputs)
	default:
		addDeclared(out, n.outputs)
	}
	return out
}

func addDeclared(out map[string]bool, decls []outDecl) {
	for _, item := range decls {
		if item.name != "" {
			out[item.name] = true
		}
	}
}

// declaresOutput reports whether output is a declared name or a path into one,
// such as items.0.id when the node declares items. This matches the runner.
func declaresOutput(names map[string]bool, output string) bool {
	for candidate := output; ; {
		if names[candidate] {
			return true
		}
		dot := strings.LastIndex(candidate, ".")
		if dot <= 0 {
			return false
		}
		candidate = candidate[:dot]
	}
}

func (g *graph) fillExports() {
	found := map[string][]string{}
	lines := map[string]int{}
	for _, n := range g.nodes {
		for name, binding := range nodeExports(n) {
			found[name] = append(found[name], binding)
			if _, ok := lines[name]; !ok {
				lines[name] = n.line
			}
		}
	}
	g.exports = map[string]string{}
	repeated := map[string]bool{}
	for name, bindings := range found {
		if len(bindings) > 1 {
			// Two nodes may publish the same name. A module infers one public
			// output per name, so only that contract is an error.
			if g.publicName(name) {
				g.add(Warning, lines[name], "output %q is declared more than once", name)
			}
			repeated[name] = true
			continue
		}
		g.exports[name] = bindings[0]
	}
	if g.public == nil {
		return
	}
	filtered := map[string]string{}
	for _, name := range g.public {
		binding, ok := g.exports[name]
		if !ok {
			if !repeated[name] {
				g.add(Error, g.line, "output %q is not declared", name)
			}
			continue
		}
		filtered[name] = binding
	}
	g.exports = filtered
}

// publicName reports whether name is part of the module contract a caller infers.
func (g *graph) publicName(name string) bool {
	if len(g.public) > 0 {
		return slices.Contains(g.public, name)
	}
	return g.role == kindModule
}

func nodeExports(n *dslNode) map[string]string {
	out := map[string]string{}
	switch n.kind {
	case kindRequest, kindSSE, kindAssert, kindWebhook, kindModule:
		for _, item := range n.outputs {
			if item.name != "" {
				out[item.name] = n.id + "." + item.name
			}
		}
	case kindSet:
		for _, item := range n.vars {
			out[item.key] = n.id + "." + item.key
		}
	}
	return out
}

func nodeTexts(n *dslNode) []string {
	var texts []string
	add := func(value any) { collectStrings(value, &texts) }
	if n.request != nil {
		add(n.request.url)
		add(pairValues(n.request.headers))
		add(pairValues(n.request.query))
		if n.request.hasBody {
			add(n.request.body)
		}
	}
	if n.module != nil {
		add(pairValues(n.module.inputs))
	}
	add(pairValues(n.vars))
	if n.assert != nil && n.assert.set {
		add(n.assert.target)
	}
	if n.branch != nil && n.branch.targetSet {
		add(n.branch.target)
	}
	if n.branch != nil {
		for _, item := range n.branch.cases {
			add(item.when.OperatorData)
			add(item.when.ExtractorData)
		}
	}
	if n.loop != nil {
		add(n.loop.items)
	}
	if n.sse != nil {
		add(n.sse.url)
		add(pairValues(n.sse.headers))
	}
	for _, item := range n.expects {
		add(item.OperatorData)
		add(item.ExtractorData)
	}
	if n.hook != nil {
		for _, group := range n.hook.groups {
			for _, item := range group.checks {
				add(item.OperatorData)
				add(item.ExtractorData)
			}
		}
	}
	return texts
}

func pairValues(pairs []pair) map[string]any {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]any, len(pairs))
	for _, item := range pairs {
		out[item.key] = item.val
	}
	return out
}

func collectStrings(value any, out *[]string) {
	switch typed := value.(type) {
	case string:
		*out = append(*out, typed)
	case map[string]any:
		for _, item := range typed {
			collectStrings(item, out)
		}
	case []any:
		for _, item := range typed {
			collectStrings(item, out)
		}
	}
}
