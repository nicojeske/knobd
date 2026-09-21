package audio

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/njeske/knobd/internal/model"
)

// fixturePath follows the convention set by
// daemon/internal/device/fixture_test.go: a hardcoded relative path from
// this package up to the repo root, since the module has no shared
// testdata-locating helper.
const fixturePath = "../../../testdata/pipewire/pw-dump-sample.json"

// fixtureNode mirrors pw-dump-sample.json's shape: a flat
// {"id": int, "props": {...}} array — NOT raw pw-dump shape (no
// "info.props" nesting). Props values are either strings or JSON
// numbers (e.g. application.process.id), and some carry a "_comment" key
// that is not a real PipeWire property (see testdata/pipewire/README.md).
type fixtureNode struct {
	ID    int                    `json:"id"`
	Props map[string]interface{} `json:"props"`
}

// loadFixtureStreams reads pw-dump-sample.json and returns every
// Stream/Output/Audio node as a Stream, sorted by ID for a deterministic
// order (tests assert "both streams of an app" pairs, which needs one).
func loadFixtureStreams(t *testing.T) []Stream {
	t.Helper()
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", fixturePath, err)
	}
	var nodes []fixtureNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		t.Fatalf("parsing fixture %s: %v", fixturePath, err)
	}

	var streams []Stream
	for _, n := range nodes {
		if n.Props["media.class"] != "Stream/Output/Audio" {
			continue
		}
		props := make(map[string]string, len(n.Props))
		for k, v := range n.Props {
			if k == "_comment" {
				continue // not a real PipeWire property
			}
			switch val := v.(type) {
			case string:
				props[k] = val
			case float64:
				// encoding/json decodes JSON numbers (e.g.
				// application.process.id) as float64; Stream.Props is
				// map[string]string, matching what the real backend's
				// PropListEntry.String() already returns.
				props[k] = strconv.FormatFloat(val, 'f', -1, 64)
			default:
				t.Fatalf("fixture node %d prop %q has unexpected type %T", n.ID, k, v)
			}
		}
		streams = append(streams, Stream{
			ID:        strconv.Itoa(n.ID),
			Direction: StreamPlayback,
			Props:     props,
		})
	}
	sort.Slice(streams, func(i, j int) bool {
		return streams[i].ID < streams[j].ID
	})
	return streams
}

func streamIDs(streams []Stream) []string {
	ids := make([]string, len(streams))
	for i, s := range streams {
		ids[i] = s.ID
	}
	return ids
}

func containsID(streams []Stream, id string) bool {
	for _, s := range streams {
		if s.ID == id {
			return true
		}
	}
	return false
}

func TestResolveFixtureEdgeCases(t *testing.T) {
	streams := loadFixtureStreams(t)
	if len(streams) != 6 {
		t.Fatalf("loadFixtureStreams: got %d streams, want 6 (112, 118, 128, 132, 146, 154)", len(streams))
	}

	t.Run("java stream (id 112) has no application.* props at all, only node.name", func(t *testing.T) {
		matcher := model.AppMatcher{ID: "java", NodeNames: []string{"java"}}
		got, err := Resolve(matcher, streams)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !containsID(got, "112") {
			t.Errorf("Resolve(NodeNames: [java]) = %v, want it to include stream 112", streamIDs(got))
		}
	})

	t.Run("a matcher with no NodeNames must not match id 112 via some other field", func(t *testing.T) {
		// id 112 has only node.name + media.name ("Playback Stream") set.
		// A matcher that doesn't key on NodeNames or that media.name must
		// not accidentally pick it up.
		matcher := model.AppMatcher{ID: "not-java", AppNames: []string{"java"}, Binaries: []string{"java"}, DesktopIDs: []string{"java"}}
		got, err := Resolve(matcher, streams)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if containsID(got, "112") {
			t.Errorf("Resolve matched stream 112 via a field other than NodeNames: %v", streamIDs(got))
		}
	})

	t.Run("vesktop (ids 118, 128) resolves to both streams", func(t *testing.T) {
		matcher := model.AppMatcher{ID: "vesktop", AppNames: []string{"vesktop"}}
		got, err := Resolve(matcher, streams)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !containsID(got, "118") || !containsID(got, "128") {
			t.Errorf("Resolve(vesktop) = %v, want both 118 and 128", streamIDs(got))
		}
		if len(got) != 2 {
			t.Errorf("Resolve(vesktop) = %v, want exactly 2 streams", streamIDs(got))
		}
	})

	t.Run("Pal (ids 132, 146) resolves to both streams despite no pid", func(t *testing.T) {
		matcher := model.AppMatcher{ID: "pal", AppNames: []string{"Pal"}}
		got, err := Resolve(matcher, streams)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !containsID(got, "132") || !containsID(got, "146") {
			t.Errorf("Resolve(Pal) = %v, want both 132 and 146", streamIDs(got))
		}
	})

	t.Run("Brave: matcher casing differs from the published property casing", func(t *testing.T) {
		// application.process.binary is "brave" (lowercase) but
		// application.name is "Brave". A matcher written against either
		// casing must still match, per the case-insensitive matching
		// decision recorded in model.AppMatcher's doc comment.
		matcher := model.AppMatcher{ID: "brave", Binaries: []string{"Brave"}}
		got, err := Resolve(matcher, streams)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !containsID(got, "154") {
			t.Errorf("Resolve(Binaries: [Brave]) = %v, want it to match stream 154 (binary=%q)", streamIDs(got), "brave")
		}
	})
}

func TestResolveEmptyMatcherMatchesNothing(t *testing.T) {
	streams := loadFixtureStreams(t)
	got, err := Resolve(model.AppMatcher{ID: "empty"}, streams)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Resolve(empty matcher) = %v, want no matches", streamIDs(got))
	}
}

func TestResolveEmptyListEntryDoesNotMatchMissingProperty(t *testing.T) {
	streams := loadFixtureStreams(t)
	// id 112 (java) has no application.name at all. A matcher with an
	// empty string in AppNames must not match it via that empty string
	// comparing equal to the missing property.
	matcher := model.AppMatcher{ID: "weird", AppNames: []string{""}}
	got, err := Resolve(matcher, streams)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Resolve(AppNames: [\"\"]) = %v, want no matches", streamIDs(got))
	}
}

func TestResolveMediaNameRxIsUnanchoredAndCaseSensitive(t *testing.T) {
	streams := loadFixtureStreams(t)

	unanchored := model.AppMatcher{ID: "pal-substring", MediaNameRx: "Pal"}
	got, err := Resolve(unanchored, streams)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !containsID(got, "132") || !containsID(got, "146") {
		t.Errorf("Resolve(MediaNameRx: Pal) = %v, want both Pal streams matched", streamIDs(got))
	}

	caseSensitive := model.AppMatcher{ID: "pal-wrong-case", MediaNameRx: "pal"}
	got, err = Resolve(caseSensitive, streams)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Resolve(MediaNameRx: pal) = %v, want no matches (regex is case-sensitive)", streamIDs(got))
	}
}

func TestResolveMediaNameRxInvalidPattern(t *testing.T) {
	_, err := Resolve(model.AppMatcher{ID: "bad-regex", MediaNameRx: "("}, nil)
	if err == nil {
		t.Fatal("expected an error for an invalid MediaNameRx pattern")
	}
}

func TestResolveNoNodeNamesFieldGivesNoFalsePositiveOnNodeName(t *testing.T) {
	// Sanity check the inverse of the id-112 negative test: a matcher
	// that DOES populate NodeNames but with an unrelated value must not
	// match java either.
	streams := loadFixtureStreams(t)
	matcher := model.AppMatcher{ID: "other", NodeNames: []string{"vesktop"}}
	got, err := Resolve(matcher, streams)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if containsID(got, "112") {
		t.Errorf("Resolve(NodeNames: [vesktop]) unexpectedly matched java: %v", streamIDs(got))
	}
}

func TestResolveCaseInsensitiveAcrossFields(t *testing.T) {
	streams := loadFixtureStreams(t)
	cases := []struct {
		name    string
		matcher model.AppMatcher
		wantID  string
	}{
		{"NodeNames folds case", model.AppMatcher{ID: "m1", NodeNames: []string{"VESKTOP"}}, "118"},
		{"AppNames folds case", model.AppMatcher{ID: "m2", AppNames: []string{"PAL"}}, "132"},
		{"Binaries folds case", model.AppMatcher{ID: "m3", Binaries: []string{"VESKTOP"}}, "118"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.matcher, streams)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !containsID(got, tc.wantID) {
				t.Errorf("Resolve(%+v) = %v, want it to include %s", tc.matcher, streamIDs(got), tc.wantID)
			}
		})
	}
}
