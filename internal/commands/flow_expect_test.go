package commands

import (
	"testing"

	"echopoint-cli/internal/api"
)

func TestParseMatch_ReadsAJSONPathCheckWithATemplateValue(t *testing.T) {
	got, err := parseMatch("$.data.invitation_id equals {{invite-member.id}}")
	if err != nil {
		t.Fatal(err)
	}
	if got.ExtractorType != "jsonPath" || got.ExtractorData["path"] != "$.data.invitation_id" ||
		got.OperatorType != "equals" || got.OperatorData["value"] != "{{invite-member.id}}" {
		t.Errorf("got %+v", got)
	}
}

func TestParseMatch_ReadsAHeaderCheckUnderTheContractKey(t *testing.T) {
	got, err := parseMatch("header:webhook-signature startsWith v1,")
	if err != nil {
		t.Fatal(err)
	}
	if got.ExtractorType != "header" || got.ExtractorData["header_name"] != "webhook-signature" {
		t.Errorf("got %+v", got)
	}
}

func TestParseMatch_KeepsSpacesInTheValue(t *testing.T) {
	got, err := parseMatch("$.message equals Invitation sent to ana")
	if err != nil {
		t.Fatal(err)
	}
	if got.OperatorData["value"] != "Invitation sent to ana" {
		t.Errorf("value=%v", got.OperatorData["value"])
	}
}

func TestParseMatch_ReadsAValuelessOperator(t *testing.T) {
	got, err := parseMatch("$.data.product_user_id notEmpty")
	if err != nil {
		t.Fatal(err)
	}
	if _, has := got.OperatorData["value"]; has {
		t.Errorf("notEmpty must carry no value: %+v", got.OperatorData)
	}
}

func TestParseMatch_RefusesMalformedChecks(t *testing.T) {
	for _, match := range []string{
		"$.type",
		"$.type is order.created",
		"type equals order.created",
		"header: equals x",
		"$.type equals",
		"$.id notEmpty oops",
	} {
		if _, err := parseMatch(match); err == nil {
			t.Errorf("%q: expected an error", match)
		}
	}
}

func TestExpectationCount(t *testing.T) {
	cases := []struct {
		name             string
		once, never      bool
		minimum, maximum int
		wantMin, wantMax *int
		wantErr          bool
	}{
		{name: "default at least once", minimum: -1, maximum: -1},
		{name: "once", once: true, minimum: -1, maximum: -1, wantMin: new(1), wantMax: new(1)},
		{name: "never", never: true, minimum: -1, maximum: -1, wantMin: new(0), wantMax: new(0)},
		{name: "range", minimum: 2, maximum: 3, wantMin: new(2), wantMax: new(3)},
		{name: "max below min", minimum: 3, maximum: 2, wantErr: true},
		{name: "two rules", once: true, never: true, minimum: -1, maximum: -1, wantErr: true},
	}
	for _, tc := range cases {
		gotMin, gotMax, err := expectationCount(tc.once, tc.never, tc.minimum, tc.maximum)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err=%v", tc.name, err)
			continue
		}
		if !samePtr(gotMin, tc.wantMin) || !samePtr(gotMax, tc.wantMax) {
			t.Errorf("%s: min=%v max=%v", tc.name, gotMin, gotMax)
		}
	}
}

func samePtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestBuildFlowNode_WebhookWait(t *testing.T) {
	node, err := buildFlowNode(nodeBuildInput{
		id: "events", nodeType: nodeTypeWebhookWait, name: "Every invitation event",
		timeoutMs: 30000, settleMs: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := node.ValueByDiscriminator()
	if err != nil {
		t.Fatal(err)
	}
	wait, ok := value.(api.WebhookWaitFlowNode)
	if !ok {
		t.Fatalf("got %T", value)
	}
	if wait.Id != "events" || *wait.Data.TimeoutMs != 30000 || *wait.Data.SettleMs != 3000 {
		t.Errorf("got %+v", wait)
	}
}
