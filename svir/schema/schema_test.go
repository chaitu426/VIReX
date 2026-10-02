package schema

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var update = flag.Bool("update", false, "rewrite testdata snapshots")

const ts = 90000

func TestExampleIsValid(t *testing.T) {
	d := Example(ts, 10)
	if err := Validate(d, true); err != nil {
		t.Fatal(err)
	}
	if d.SchemaMajor != VersionMajor || d.SchemaMinor != VersionMinor {
		t.Fatalf("version %d.%d", d.SchemaMajor, d.SchemaMinor)
	}
}

func TestProtobufRoundTrip(t *testing.T) {
	d := Example(ts, 10)
	b, err := Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(d, got) {
		t.Fatal("document changed in a Protobuf round trip")
	}
	// Deterministic output: same document, same bytes.
	b2, _ := Marshal(d)
	if string(b) != string(b2) {
		t.Fatal("Marshal is not deterministic")
	}
}

func TestEmptyDocument(t *testing.T) {
	d, err := Unmarshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(d, true); err != nil {
		t.Fatalf("empty document must be valid: %v", err)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	d := Example(ts, 10)
	j, err := ToJSON(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := FromJSON(j)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(d, got) {
		t.Fatal("document changed in a JSON round trip")
	}
	if !strings.Contains(string(j), `"pixel_ref"`) || !strings.Contains(string(j), `"layer"`) {
		t.Fatal("JSON export is missing pixel_ref or layer")
	}
}

func TestEveryBlockHasLayerAndPixelRef(t *testing.T) {
	d := Example(ts, 10)
	n := 0
	for _, b := range blocks(d) {
		n++
		if b.GetLayer() == Layer_LAYER_UNSPECIFIED || b.GetPixelRef() == nil {
			t.Fatalf("%T lacks layer or pixel_ref", b)
		}
	}
	if n < 10 {
		t.Fatalf("example should cover every block type, has %d blocks", n)
	}
}

func TestDefaultLayers(t *testing.T) {
	want := map[Layer]int{Layer_L0_ENTITY: 5, Layer_L1_EVENT: 6, Layer_L2_TEXT: 1, Layer_L3_EMBEDDING: 1}
	got := map[Layer]int{}
	for _, b := range blocks(Example(ts, 10)) {
		got[b.GetLayer()]++
	}
	for l, n := range want {
		if got[l] != n {
			t.Fatalf("layer %v: %d blocks, want %d (all: %v)", l, got[l], n, got)
		}
	}
}

func TestValidateRejects(t *testing.T) {
	mut := map[string]func(*SVIRDocument){
		"no layer":          func(d *SVIRDocument) { d.Objects[0].Layer = Layer_LAYER_UNSPECIFIED },
		"no pixel_ref":      func(d *SVIRDocument) { d.Texts[0].PixelRef = nil },
		"reversed pixelref": func(d *SVIRDocument) { d.Scenes[0].PixelRef.TStart, d.Scenes[0].PixelRef.TEnd = 10, 5 },
		"bbox out of range": func(d *SVIRDocument) { d.Objects[0].Bbox.X = 1.5 },
		"bbox past frame":   func(d *SVIRDocument) { d.Objects[0].Bbox = &BBox{X: 0.8, Y: 0, W: 0.5, H: 0.1} },
		"bad confidence":    func(d *SVIRDocument) { v := float32(1.2); d.Objects[0].Confidence = &v },
		"action reversed":   func(d *SVIRDocument) { d.Actions[0].Start, d.Actions[0].End = 9, 1 },
		"unknown entity":    func(d *SVIRDocument) { d.Objects[0].EntityId = 999 },
		"unknown relation":  func(d *SVIRDocument) { d.Relations[0].ObjectId = 999 },
		"attribute entity":  func(d *SVIRDocument) { d.Attributes[0].EntityId = 999 },
		"unknown rel id":    func(d *SVIRDocument) { d.Temporals[1].AddedRelationIds = []uint64{42} },
		"duplicate rel id": func(d *SVIRDocument) {
			d.Relations = append(d.Relations, &RelationBlock{Id: 1, SubjectId: 1, ObjectId: 2, Layer: Layer_L1_EVENT, PixelRef: &PixelRef{}})
		},
		"entity reversed": func(d *SVIRDocument) { d.Entities[0].FirstSeen, d.Entities[0].LastSeen = 5, 1 },
	}
	for name, m := range mut {
		t.Run(name, func(t *testing.T) {
			d := Example(ts, 10)
			m(d)
			if Validate(d, true) == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestSplitAndMerge(t *testing.T) {
	d := Example(ts, 10)
	parts := SplitByLayer(d)
	if len(parts) != 4 {
		t.Fatalf("got %d layers", len(parts))
	}
	var docs []*SVIRDocument
	for l, p := range parts {
		if p.Layer != l {
			t.Fatalf("part stamped %v, want %v", p.Layer, l)
		}
		for _, b := range blocks(p) {
			if b.GetLayer() != l {
				t.Fatalf("%T with layer %v ended up in %v", b, b.GetLayer(), l)
			}
		}
		// Each part must survive its own serialisation, as a SEMANTIC section would.
		raw, _ := Marshal(p)
		back, err := Unmarshal(raw)
		if err != nil || !proto.Equal(p, back) {
			t.Fatalf("layer %v round trip failed: %v", l, err)
		}
		docs = append(docs, back)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Layer < docs[j].Layer })
	m := Merge(docs...)
	if len(blocks(m)) != len(blocks(d)) {
		t.Fatalf("merge has %d blocks, original %d", len(blocks(m)), len(blocks(d)))
	}
	if err := Validate(m, true); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownFieldsSurvive(t *testing.T) {
	// A newer writer may add fields. An older reader must keep them, not fail.
	d := Example(ts, 10)
	b, _ := Marshal(d)
	b = append(b, 0xF8, 0x06, 0x2A) // field 111, varint 42: unknown to v0.1
	got, err := Unmarshal(b)
	if err != nil {
		t.Fatalf("old reader rejected a newer file: %v", err)
	}
	if len(got.ProtoReflect().GetUnknown()) == 0 {
		t.Fatal("unknown field was dropped")
	}
}

// fieldSnapshot lists every field as "Message.field = number type cardinality".
func fieldSnapshot() []string {
	var lines []string
	files := File_svir_proto
	var walk func(ms protoreflect.MessageDescriptors)
	walk = func(ms protoreflect.MessageDescriptors) {
		for i := 0; i < ms.Len(); i++ {
			m := ms.Get(i)
			for j := 0; j < m.Fields().Len(); j++ {
				f := m.Fields().Get(j)
				kind := f.Kind().String()
				if f.Message() != nil {
					kind = string(f.Message().FullName())
				}
				if f.Enum() != nil {
					kind = string(f.Enum().FullName())
				}
				lines = append(lines, fmt.Sprintf("%s.%s = %d %s %s", m.Name(), f.Name(), f.Number(), kind, f.Cardinality()))
			}
			walk(m.Messages())
		}
	}
	walk(files.Messages())
	for i := 0; i < files.Enums().Len(); i++ {
		e := files.Enums().Get(i)
		for j := 0; j < e.Values().Len(); j++ {
			v := e.Values().Get(j)
			lines = append(lines, fmt.Sprintf("enum %s.%s = %d", e.Name(), v.Name(), v.Number()))
		}
	}
	sort.Strings(lines)
	return lines
}

// TestSchemaOnlyGrows is the freeze guard. testdata/fields_v0_1.txt records every field
// number, type and enum value of v0.1. Later versions may ADD lines, but changing or
// removing a recorded line breaks old files, so this test fails. Run with -update only
// when deliberately adding fields.
func TestSchemaOnlyGrows(t *testing.T) {
	path := filepath.Join("testdata", "fields_v0_1.txt")
	cur := fieldSnapshot()
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(cur, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("missing snapshot (run: go test ./svir/schema -update): %v", err)
	}
	defer f.Close()
	have := map[string]bool{}
	for _, l := range cur {
		have[l] = true
	}
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" {
			continue
		}
		n++
		if !have[l] {
			t.Errorf("v0.1 field changed or removed: %q", l)
		}
	}
	if n == 0 {
		t.Fatal("empty snapshot")
	}
}
