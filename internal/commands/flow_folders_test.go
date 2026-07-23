package commands

import (
	"strings"
	"testing"

	"echopoint-cli/internal/api"

	"github.com/google/uuid"
)

// folderFixture builds a small tree:
//
//	Anchor/
//	  Identity/
//	    Roles
//	  Products
//	Echopoint/
//	  Identity        <- same leaf name as Anchor/Identity, different parent
func folderFixture() (*folderIndex, map[string]uuid.UUID) {
	ids := map[string]uuid.UUID{
		"anchor":           uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		"anchor/identity":  uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		"anchor/roles":     uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		"anchor/products":  uuid.MustParse("44444444-4444-4444-8444-444444444444"),
		"echopoint":        uuid.MustParse("55555555-5555-4555-8555-555555555555"),
		"echopoint/ident":  uuid.MustParse("66666666-6666-4666-8666-666666666666"),
		"missing-from-org": uuid.MustParse("77777777-7777-4777-8777-777777777777"),
	}

	folder := func(id uuid.UUID, name string, parent *uuid.UUID) api.FlowFolder {
		return api.FlowFolder{Id: id, Name: name, ParentId: parent}
	}
	anchor := ids["anchor"]
	anchorIdentity := ids["anchor/identity"]
	echopoint := ids["echopoint"]

	return newFolderIndex([]api.FlowFolder{
		folder(ids["anchor"], "Anchor", nil),
		folder(ids["anchor/identity"], "Identity", &anchor),
		folder(ids["anchor/roles"], "Roles", &anchorIdentity),
		folder(ids["anchor/products"], "Products", &anchor),
		folder(ids["echopoint"], "Echopoint", nil),
		folder(ids["echopoint/ident"], "Identity", &echopoint),
	}), ids
}

func TestFolderIndexResolve(t *testing.T) {
	idx, ids := folderFixture()

	t.Run("resolves paths", func(t *testing.T) {
		cases := map[string]string{
			"Anchor":                 "anchor",
			"Anchor/Identity":        "anchor/identity",
			"Anchor/Identity/Roles":  "anchor/roles",
			"/Anchor/Products/":      "anchor/products",
			"anchor/identity/roles":  "anchor/roles",
			"  Echopoint/Identity  ": "echopoint/ident",
		}
		for ref, want := range cases {
			got, err := idx.resolve(ref)
			if err != nil {
				t.Fatalf("resolve(%q): unexpected error: %v", ref, err)
			}
			if got != ids[want] {
				t.Errorf("resolve(%q) = %s, want %s (%s)", ref, got, ids[want], want)
			}
		}
	})

	t.Run("resolves a known id", func(t *testing.T) {
		got, err := idx.resolve(ids["anchor/roles"].String())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != ids["anchor/roles"] {
			t.Errorf("got %s, want %s", got, ids["anchor/roles"])
		}
	})

	t.Run("rejects an id from another organization", func(t *testing.T) {
		_, err := idx.resolve(ids["missing-from-org"].String())
		if err == nil || !strings.Contains(err.Error(), "no folder with id") {
			t.Errorf("expected an unknown-id error, got %v", err)
		}
	})

	t.Run("rejects an empty reference", func(t *testing.T) {
		for _, ref := range []string{"", "   ", "/"} {
			if _, err := idx.resolve(ref); err == nil {
				t.Errorf("resolve(%q): expected an error", ref)
			}
		}
	})

	t.Run("rejects a missing segment and names the parent", func(t *testing.T) {
		_, err := idx.resolve("Anchor/Nope")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), `no folder named "Nope"`) || !strings.Contains(err.Error(), "Anchor") {
			t.Errorf("error should name the segment and its parent, got %v", err)
		}
	})

	t.Run("does not match a nested folder from the root", func(t *testing.T) {
		if _, err := idx.resolve("Identity"); err == nil {
			t.Error("expected 'Identity' not to resolve at the root")
		}
	})
}

func TestFolderIndexResolveAmbiguous(t *testing.T) {
	first := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	second := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	idx := newFolderIndex([]api.FlowFolder{
		{Id: first, Name: "Anchor"},
		{Id: second, Name: "anchor"},
	})

	_, err := idx.resolve("Anchor")
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	if !strings.Contains(err.Error(), "2 folders named") || !strings.Contains(err.Error(), "folder id") {
		t.Errorf("error should report the ambiguity and suggest an id, got %v", err)
	}
}

func TestFolderIndexPath(t *testing.T) {
	idx, ids := folderFixture()

	cases := map[string]string{
		"anchor":          "Anchor",
		"anchor/identity": "Anchor/Identity",
		"anchor/roles":    "Anchor/Identity/Roles",
		"echopoint/ident": "Echopoint/Identity",
	}
	for key, want := range cases {
		if got := idx.path(ids[key]); got != want {
			t.Errorf("path(%s) = %q, want %q", key, got, want)
		}
	}

	if got := idx.path(uuid.Nil); got != "" {
		t.Errorf("path(root) = %q, want an empty string", got)
	}
}

func TestFolderIndexPathStopsOnCycle(t *testing.T) {
	first := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	second := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	idx := newFolderIndex([]api.FlowFolder{
		{Id: first, Name: "A", ParentId: &second},
		{Id: second, Name: "B", ParentId: &first},
	})

	// The server rejects cycles; the client must still terminate on bad data.
	if got := idx.path(first); got == "" {
		t.Errorf("expected a best-effort path, got %q", got)
	}
}

func TestFolderIndexAddReparents(t *testing.T) {
	idx, ids := folderFixture()

	root := ids["echopoint"]
	moved := api.FlowFolder{Id: ids["anchor/identity"], Name: "Identity", ParentId: &root}
	idx.add(moved)

	if got := idx.path(ids["anchor/identity"]); got != "Echopoint/Identity" {
		t.Errorf("after add, path = %q, want %q", got, "Echopoint/Identity")
	}
	if _, matches := idx.lookupChild(ids["anchor"], "Identity"); matches != 0 {
		t.Errorf("the folder should no longer be a child of Anchor, matches = %d", matches)
	}
	if _, matches := idx.lookupChild(root, "Identity"); matches != 2 {
		t.Errorf("Echopoint should now hold both Identity folders, matches = %d", matches)
	}
}

func TestFolderIndexWalkOrder(t *testing.T) {
	idx, _ := folderFixture()

	var lines []string
	idx.walk(uuid.Nil, 0, func(folder api.FlowFolder, depth int) {
		lines = append(lines, strings.Repeat("  ", depth)+folder.Name)
	})

	want := []string{"Anchor", "  Identity", "    Roles", "  Products", "Echopoint", "  Identity"}
	if len(lines) != len(want) {
		t.Fatalf("walk visited %d folders, want %d: %v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestSplitFolderPath(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"Anchor", []string{"Anchor"}},
		{"Anchor/Identity", []string{"Anchor", "Identity"}},
		{"/Anchor//Identity/", []string{"Anchor", "Identity"}},
		{" Anchor / Identity ", []string{"Anchor", "Identity"}},
		{"", nil},
		{"///", nil},
	}
	for _, tc := range cases {
		got := splitFolderPath(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitFolderPath(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("splitFolderPath(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestFolderDestinationRefs(t *testing.T) {
	t.Run("uncategorized", func(t *testing.T) {
		for _, ref := range []string{"uncategorized", "Uncategorized", " UNCATEGORIZED "} {
			if !isUncategorizedRef(ref) {
				t.Errorf("isUncategorizedRef(%q) = false, want true", ref)
			}
		}
		if isUncategorizedRef("Anchor") {
			t.Error(`isUncategorizedRef("Anchor") = true, want false`)
		}
	})

	t.Run("root", func(t *testing.T) {
		for _, ref := range []string{"", "  ", "/", "root", "ROOT"} {
			if !isRootRef(ref) {
				t.Errorf("isRootRef(%q) = false, want true", ref)
			}
		}
		if isRootRef("Anchor") {
			t.Error(`isRootRef("Anchor") = true, want false`)
		}
	})

	t.Run("describes the destination", func(t *testing.T) {
		if got := describeDestination("uncategorized"); got != "the uncategorized bucket" {
			t.Errorf("got %q", got)
		}
		if got := describeDestination("/Anchor/Identity/"); got != `"Anchor/Identity"` {
			t.Errorf("got %q", got)
		}
	})
}

func TestFolderCountCell(t *testing.T) {
	if got := folderCountCell(true, 3); got != "3" {
		t.Errorf("got %q, want %q", got, "3")
	}
	if got := folderCountCell(false, 3); got != "" {
		t.Errorf("got %q, want an empty cell", got)
	}
}
