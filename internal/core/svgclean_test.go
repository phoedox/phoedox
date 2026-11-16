package core

import (
	"strings"
	"testing"
)

func TestCleanSVG(t *testing.T) {
	in := `<?xml version="1.0"?><!DOCTYPE svg><svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:inkscape="http://www.inkscape.org/namespaces/inkscape" viewBox="0 0 10 10" onload="alert(1)" inkscape:version="1">
<script>alert(1)</script><style>@import url(https://evil)</style>
<defs><linearGradient id="g"><stop offset="0" stop-color="#f00"/></linearGradient></defs>
<a href="https://evil"><rect width="10" height="10" fill="url(#g)" onclick="x()" style="fill:url(https://evil/x)"/></a>
<image href="https://evil/p.png"/><image xlink:href="data:image/png;base64,iVBORw0KGgo="/>
<use xlink:href="#g"/><use href="https://evil#x"/>
<foreignObject><div xmlns="http://www.w3.org/1999/xhtml">x</div></foreignObject>
<text x="1" y="5">A &amp; B &lt;</text></svg>`
	out, err := cleanSVG([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, bad := range []string{"script", "onload", "onclick", "@import", "evil", "foreignObject", "inkscape", "<style", "<a", "DOCTYPE", "div"} {
		if strings.Contains(s, bad) {
			t.Fatalf("%q left in %s", bad, s)
		}
	}
	for _, good := range []string{`<rect width="10" height="10" fill="url(#g)">`, `xlink:href="data:image/png;base64,`, `<use xlink:href="#g">`, `A &amp; B &lt;`, `viewBox="0 0 10 10"`, `<stop offset="0" stop-color="#f00">`} {
		if !strings.Contains(s, good) {
			t.Fatalf("%q missing in %s", good, s)
		}
	}
	for _, notSVG := range []string{`<html><svg/></html>`, `not xml`, `<svg xmlns="http://www.w3.org/2000/svg"><rect></svg>`} {
		if _, err := cleanSVG([]byte(notSVG)); err == nil {
			t.Fatalf("accepted %q", notSVG)
		}
	}
}
