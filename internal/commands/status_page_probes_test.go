package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
)

func TestStatusPageProbeBindingsValidateSaveAndReadBack(t *testing.T) {
	fixture, err := os.ReadFile("testdata/status-page.json")
	if err != nil {
		t.Fatal(err)
	}
	var request api.SaveStatusPageRequest
	if err := json.Unmarshal(fixture, &request); err != nil {
		t.Fatal(err)
	}
	bindings := []api.StatusPageProbeBinding{
		{ServiceId: "api", ProbeId: testProbeID, CapabilityId: "api"},
		{ServiceId: "api", ProbeId: testProbeID, CapabilityId: "webhook"},
	}
	request.ProbeBindings = &bindings
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, newStatusPageCmd(&AppState{}), string(data), "validate", "-f", "-"); err != nil {
		t.Fatal(err)
	}
	var saved *api.StatusPageEditor
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Organization-ID") != "org_test" {
			t.Error("tenant scope missing")
		}
		if r.Method == http.MethodPut {
			var input api.SaveStatusPageRequest
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(input.ProbeBindings, request.ProbeBindings) {
				t.Error("probe bindings dropped on save")
			}
			saved = &api.StatusPageEditor{
				ProbeBindings: input.ProbeBindings,
				DraftVersion:  1,
				Slug:          "example-a1b2c3d4e5f6",
				Config:        input.Config,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(saved)
	}))
	defer server.Close()
	state := makeState(t, "test-key", "", server.URL)
	if _, _, err := execute(t, newStatusPageCmd(state), string(data), "save", "-f", "-"); err != nil {
		t.Fatal(err)
	}
	out, _, err := execute(t, newStatusPageCmd(state), "", "view")
	if err != nil {
		t.Fatal(err)
	}
	var readBack api.StatusPageEditor
	if err := json.Unmarshal([]byte(out), &readBack); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(readBack.ProbeBindings, &bindings) {
		t.Fatalf("bindings = %+v", readBack.ProbeBindings)
	}
	for _, bad := range []string{
		strings.Replace(string(data), `"capability_id":"api"`, `"capability_id":""`, 1),
		strings.Replace(string(data), `"capability_id":"api"`, `"capability_id":"api","revision":1`, 1),
	} {
		cmd := newStatusPageCmd(&AppState{})
		cmd.SetOut(io.Discard)
		if _, _, err := execute(t, cmd, bad, "validate", "-f", "-"); err == nil {
			t.Fatalf("invalid mapping accepted: %s", bad)
		}
	}
}
