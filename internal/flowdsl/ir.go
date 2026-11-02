package flowdsl

import (
	"fmt"
	"strconv"

	"echopoint-cli/internal/api"
)

// Severity is how serious a diagnostic is. A warning still compiles.
type Severity int

const (
	// Error means the file does not compile.
	Error Severity = iota
	// Warning means the file compiles, with something the author should see.
	Warning
)

// Diagnostic is one problem found while reading or checking a flow file.
type Diagnostic struct {
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Severity Severity `json:"-"`
	Message  string   `json:"message"`
}

func (d Diagnostic) String() string {
	where := d.File
	if d.Line > 0 && where != "" {
		where = where + ":" + strconv.Itoa(d.Line)
	}
	msg := d.Message
	if where != "" {
		msg = where + ": " + msg
	}
	if d.Severity == Warning {
		return "warning: " + msg
	}
	return msg
}

// Flow is one compiled create-flow document.
type Flow struct {
	// Path is the file's module path, such as anchor/login.
	Path    string                `json:"path"`
	Request api.CreateFlowRequest `json:"request"`
}

// Outcome is everything a compile found. Flows with an error are omitted.
type Outcome struct {
	Flows       []Flow
	Diagnostics []Diagnostic
}

// Errors returns the diagnostics that failed the compile.
func (o Outcome) Errors() []Diagnostic {
	return o.filter(Error)
}

// Warnings returns the diagnostics that did not fail the compile.
func (o Outcome) Warnings() []Diagnostic {
	return o.filter(Warning)
}

func (o Outcome) filter(sev Severity) []Diagnostic {
	var out []Diagnostic
	for _, d := range o.Diagnostics {
		if d.Severity == sev {
			out = append(out, d)
		}
	}
	return out
}

func (o Outcome) filterPaths(keep map[string]bool) Outcome {
	var next Outcome
	for _, flow := range o.Flows {
		if keep[flow.Path] {
			next.Flows = append(next.Flows, flow)
		}
	}
	for _, d := range o.Diagnostics {
		if d.File == "" || keep[d.File] {
			next.Diagnostics = append(next.Diagnostics, d)
		}
	}
	return next
}

const defaultFlowVersion = "1.0"

// statusWord is the YAML spelling of a status_code extractor, both as an
// expect target (`status between 200 299`) and as an output (`status: status`).
const statusWord = "status"

const (
	kindRequest = "request"
	kindModule  = "module"
	kindDelay   = "delay"
	kindSet     = "set_variable"
	kindAssert  = "assert"
	kindBranch  = "branch"
	kindPoll    = "poll"
	kindLoop    = "loop"
	kindWebhook = "webhook_wait"
	kindSSE     = "sse"
)

// kindKey is the YAML key an author writes. set is the file's spelling of a
// set_variable node.
var kindKeys = []string{
	"request", kindModule, "delay", "set", "assert", "branch", "poll", "loop", "webhook_wait", "sse",
}

func canonicalKind(key string) string {
	if key == "set" {
		return kindSet
	}
	return key
}

type graph struct {
	path        string
	inline      bool
	line        int
	name        string
	description string
	role        string
	version     string
	tags        []string
	id          string
	folder      string
	folderLine  int
	inputs      []string
	public      []string
	nodes       []*dslNode
	fromSteps   bool
	exports     map[string]string
	injected    []string // loop item and index names visible inside a body
	diags       []Diagnostic
}

func (g *graph) add(sev Severity, line int, format string, args ...any) {
	g.diags = append(g.diags, Diagnostic{
		File:     g.path,
		Line:     line,
		Severity: sev,
		Message:  fmt.Sprintf(format, args...),
	})
}

func (g *graph) collect() []Diagnostic {
	out := append([]Diagnostic{}, g.diags...)
	for _, n := range g.nodes {
		out = append(out, nestedDiags(n)...)
	}
	return out
}

func nestedDiags(n *dslNode) []Diagnostic {
	var out []Diagnostic
	if n.poll != nil && n.poll.body != nil {
		out = append(out, n.poll.body.collect()...)
	}
	if n.loop != nil && n.loop.body != nil {
		out = append(out, n.loop.body.collect()...)
	}
	return out
}

func (g *graph) hasError() bool {
	for _, d := range g.collect() {
		if d.Severity == Error {
			return true
		}
	}
	return false
}

type dslNode struct {
	id      string
	line    int
	display string
	runWhen string
	next    []string
	nextSet bool
	fail    []string
	kind    string
	outputs []outDecl
	expects []api.CompositeAssertion
	request *reqSpec
	module  *modSpec
	delayMS int
	vars    []pair
	assert  *assertSpec
	branch  *branchSpec
	poll    *pollSpec
	loop    *loopSpec
	hook    *hookSpec
	sse     *sseSpec
}

type outDecl struct {
	name      string
	binding   string
	extractor string
	path      string
	header    string
	secret    bool
	line      int
}

type pair struct {
	key string
	val any
}

type reqSpec struct {
	method  string
	url     string
	headers []pair
	query   []pair
	body    any
	hasBody bool
	timeout *int
}

type modSpec struct {
	ref      string
	refLine  int
	flowID   string
	inputs   []pair
	explicit bool
}

type assertSpec struct {
	target any
	set    bool
}

type branchCase struct {
	when   api.CompositeAssertion
	target string
	line   int
}

type branchSpec struct {
	target        any
	targetSet     bool
	cases         []branchCase
	defaultTarget string
	hasDefault    bool
}

type pollSpec struct {
	maxAttempts *int
	interval    *int
	timeout     *int
	body        *graph
}

type loopSpec struct {
	items      any
	itemVar    string
	indexVar   string
	max        *int
	continueOn *bool
	body       *graph
}

type hookGroup struct {
	name   string
	min    *int
	max    *int
	checks []api.CompositeAssertion
}

type hookSpec struct {
	timeout *int
	settle  *int
	groups  []hookGroup
}

type sseSpec struct {
	url        string
	method     string
	headers    []pair
	timeout    *int
	maxEvents  *int
	completion *string
	stop       *bool
}
