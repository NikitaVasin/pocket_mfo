package typedconfig

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"
)

var updateDart = flag.Bool("update-typedconfig-dart", false, "regenerate the typedconfig Dart fixture")

func TestGenerateDart(t *testing.T) {
	var b bytes.Buffer
	must(t, GenerateDart(&b, schema("source"), "Screen"))
	path := "testdata/models.dart"
	if *updateDart {
		must(t, os.WriteFile(path, b.Bytes(), 0600))
	}
	golden, err := os.ReadFile(path)
	must(t, err)
	if b.String() != string(golden) {
		t.Fatal("Dart fixture differs; use -update-typedconfig-dart")
	}
	for _, invalid := range []Schema{{}, {Version: 1, Types: []Block{{Key: "bad", Fields: []Property{{Name: "bad", Node: Node{Ref: "missing"}}}}}}} {
		b.Reset()
		if err := GenerateDart(&b, invalid, "Screen"); err == nil || b.Len() != 0 {
			t.Fatal("invalid schema produced output")
		}
	}
	b.Reset()
	if err := GenerateDart(&b, schema("source"), "bad-name"); err == nil {
		t.Fatal("invalid class accepted")
	}
	if !strings.Contains(string(golden), "List<ScreenGroup3DataOffers1Entry>") {
		t.Fatal("nested reference list is untyped")
	}
}
