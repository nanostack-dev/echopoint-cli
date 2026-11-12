package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
)

func nodeDoc(t *testing.T, node api.FlowNode) map[string]any {
	t.Helper()
	raw, err := node.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestBuildFlowNodeEncodesAnEmailWait(t *testing.T) {
	always := api.FlowNodeRunWhen("always")
	node, err := buildFlowNode(nodeBuildInput{
		id: "verify", nodeType: nodeTypeEmailWait, name: "Verification email",
		timeoutMs: 90000, settleMs: 1000, linkPattern: "/verify", codePattern: `code (\d+)`, runWhen: &always,
	})
	if err != nil {
		t.Fatal(err)
	}
	if kind, _ := node.Discriminator(); kind != nodeTypeEmailWait {
		t.Fatalf("type=%s", kind)
	}
	doc := nodeDoc(t, node)
	data, _ := doc["data"].(map[string]any)
	if doc["id"] != "verify" || doc["run_when"] != "always" || data["timeout_ms"] != float64(90000) ||
		data["settle_ms"] != float64(
			1000,
		) || data["link_pattern"] != "/verify" || data["code_pattern"] != `code (\d+)` {
		t.Errorf("node=%v", doc)
	}
}

func TestBuildFlowNodeRefusesABadEmailWait(t *testing.T) {
	for name, in := range map[string]nodeBuildInput{
		"timeout past the address lifetime": {timeoutMs: maxEmailWaitTimeoutMs + 1},
		"bad link pattern":                  {linkPattern: "("},
		"bad code pattern":                  {codePattern: "[a-"},
	} {
		t.Run(name, func(t *testing.T) {
			in.id, in.nodeType, in.name = "verify", nodeTypeEmailWait, "Verify"
			if _, err := buildFlowNode(in); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func emailWaitNode(t *testing.T) api.FlowNode {
	t.Helper()
	node, err := buildFlowNode(nodeBuildInput{id: "verify", nodeType: nodeTypeEmailWait, name: "Verify"})
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func TestEmailWaitNodeTakesAssertionsOutputsAndExpectedEvents(t *testing.T) {
	node := emailWaitNode(t)
	if !isWaitNode(node, "verify") || isWaitNode(node, "other") {
		t.Fatal("isWaitNode must find the email wait by id")
	}
	check, err := parseMatch(`html:a.verify@href starts_with https://`)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendNodeAssertion(&node, check); err != nil {
		t.Fatal(err)
	}
	output := buildNodeOutput("code", "json_path", "$.code", "", true)
	if err := appendRawOutput(&node, output, nil); err != nil {
		t.Fatal(err)
	}
	link := buildNodeOutput("reset", string(extractorTypeHTML), "", "", false)
	if err := appendRawOutput(&node, link, map[string]any{"selector": "a.reset", "attribute": "href"}); err != nil {
		t.Fatal(err)
	}
	subject, err := parseMatch(`$.subject contains Verify`)
	if err != nil {
		t.Fatal(err)
	}
	err = editNodeExpectations(&node, func(expect *[]api.WebhookExpectation) error {
		*expect = append(
			*expect,
			api.WebhookExpectation{Name: "verification", Assertions: []api.CompositeAssertion{subject}},
		)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := node.MarshalJSON()
	text := string(raw)
	for _, want := range []string{
		`"type":"email_wait"`,
		`"extractor_data":{"attribute":"href","selector":"a.verify"},"extractor_type":"html"`,
		`"name":"code","secret":true`,
		`"extractor":{"attribute":"href","selector":"a.reset","type":"html"}`,
		`"expect":[{"assertions":[{"extractor_data":{"path":"$.subject"}`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("node lacks %s\n%s", want, text)
		}
	}
}

func TestParseMatchReadsHTMLAndRegexTargets(t *testing.T) {
	text, err := parseMatch(`html:h1 contains Welcome`)
	if err != nil || text.ExtractorType != extractorTypeHTML || text.ExtractorData["selector"] != "h1" ||
		text.ExtractorData["attribute"] != nil {
		t.Errorf("html text: %+v %v", text, err)
	}
	attr, err := parseMatch(`html:a[href*="@x"]@href not_empty`)
	if err != nil || attr.ExtractorData["selector"] != `a[href*="@x"]` || attr.ExtractorData["attribute"] != "href" {
		t.Errorf("html attribute: %+v %v", attr, err)
	}
	code, err := parseMatch(`regex:(\d{6}) equals 482913`)
	if err != nil || code.ExtractorType != extractorTypeRegex || code.ExtractorData["pattern"] != `(\d{6})` {
		t.Errorf("regex: %+v %v", code, err)
	}
	if _, err := parseMatch(`regex:( equals x`); err == nil {
		t.Error("a bad regex must be refused")
	}
}

func TestValidateAcceptsInboxLaunchInputs(t *testing.T) {
	for _, key := range []string{"inbox.address", "inbox.local", "inbox.domain", "inbox.messages_url"} {
		if !launchInputs[key] {
			t.Errorf("%s is a launch input", key)
		}
	}
}

func TestRemoveNodeAssertionOnAnEmailWait(t *testing.T) {
	node := emailWaitNode(t)
	for _, match := range []string{"$.subject contains A", "$.subject contains B"} {
		check, _ := parseMatch(match)
		if err := appendNodeAssertion(&node, check); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeNodeAssertion(&node, 0); err != nil {
		t.Fatal(err)
	}
	raw, _ := node.MarshalJSON()
	if strings.Contains(string(raw), `"value":"A"`) || !strings.Contains(string(raw), `"value":"B"`) {
		t.Errorf("node=%s", raw)
	}
	if err := removeNodeAssertion(&node, 5); err == nil {
		t.Error("an index out of range must be refused")
	}
}
