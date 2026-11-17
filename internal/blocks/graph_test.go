package blocks

import "testing"

func TestGraphBlocks(t *testing.T) {
	tr, ps := ParseTree([]byte("root: @router\nrelation: uplink\ndirection: out\ndepth: 3"))
	if len(ps) > 0 || tr.Root != "router" || tr.Direction != "out" || tr.Relation != "uplink" {
		t.Fatalf("tree: %+v %v", tr, ps)
	}
	if tr, _ := ParseTree([]byte("@oma")); tr.Root != "oma" || tr.Direction != "in" {
		t.Fatalf("bare root: %+v", tr)
	}
	if r, ps := ParseRack([]byte("@rack-keller")); r != "rack-keller" || len(ps) > 0 {
		t.Fatalf("rack: %q %v", r, ps)
	}
	if p, r, ps := ParsePlan([]byte("plan: erdgeschoss\nroom: Küche")); p != "erdgeschoss" || r != "Küche" || len(ps) > 0 {
		t.Fatalf("plan: %q %q %v", p, r, ps)
	}
	for name, c := range map[string][2]string{
		"tree no root":  {"tree", "relation: uplink"},
		"tree dir":      {"tree", "root: @a\ndirection: sideways"},
		"rack prose":    {"rack", "Rack im Keller"},
		"rack empty":    {"rack", ""},
		"plan no plan":  {"floorplan", "room: Küche"},
		"plan bad slug": {"floorplan", "plan: Erd Geschoss"},
	} {
		if len(Check(c[0], []byte(c[1]))) == 0 {
			t.Errorf("%s: no problem", name)
		}
	}
	for _, k := range []string{"tree", "rack", "floorplan"} {
		if d, _ := Get(k); !d.Data || !d.Board {
			t.Errorf("%s: data and board expected", k)
		}
	}
}
