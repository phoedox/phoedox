package graph

import (
	"reflect"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Router Flur":             "router-flur",
		"LAN-Dose AZ-1":           "lan-dose-az-1",
		"Küche (PoE)":             "kueche-poe",
		"  Serverschrank Keller ": "serverschrank-keller",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
		if err := ValidateSlug(Slugify(in)); err != nil {
			t.Errorf("Slugify(%q) produced invalid slug: %v", in, err)
		}
	}
}

func TestParseMentions(t *testing.T) {
	got := ParseMentions("Der @router-flur hängt am @lindenstrasse/smart-plug-flur. Mail: a@b")
	want := []Mention{{"", "router-flur"}, {"lindenstrasse", "smart-plug-flur"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestNewIDUnique(t *testing.T) {
	a, b := NewID(), NewID()
	if a == b || len(a) != 26 {
		t.Fatalf("unexpected ids %q %q", a, b)
	}
}
