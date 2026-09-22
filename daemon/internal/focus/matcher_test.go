package focus

import (
	"slices"
	"strings"
	"testing"
)

func containsFold(tokens []string, want string) bool {
	for _, t := range tokens {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}

func TestNameCandidatesBraveBrowser(t *testing.T) {
	info := AppInfo{ResourceClass: "brave-browser"}
	got := info.NameCandidates()
	if !containsFold(got, "brave") {
		t.Fatalf("NameCandidates(%+v) = %v, want it to contain %q", info, got, "brave")
	}
}

func TestNameCandidatesGoogleChrome(t *testing.T) {
	info := AppInfo{ResourceClass: "google-chrome"}
	got := info.NameCandidates()
	if !containsFold(got, "chrome") {
		t.Fatalf("NameCandidates(%+v) = %v, want it to contain %q", info, got, "chrome")
	}
}

func TestNameCandidatesVesktopOnlyItself(t *testing.T) {
	info := AppInfo{ResourceClass: "vesktop"}
	got := info.NameCandidates()
	want := []string{"vesktop"}
	if !slices.Equal(got, want) {
		t.Fatalf("NameCandidates(%+v) = %v, want %v (no dash/dot to normalize)", info, got, want)
	}
}

func TestNameCandidatesDesktopFileIDReverseDNS(t *testing.T) {
	info := AppInfo{DesktopFileID: "org.kde.dolphin.desktop"}
	got := info.NameCandidates()
	if !containsFold(got, "org.kde.dolphin") {
		t.Fatalf("NameCandidates(%+v) = %v, want it to contain %q", info, got, "org.kde.dolphin")
	}
	if !containsFold(got, "dolphin") {
		t.Fatalf("NameCandidates(%+v) = %v, want it to contain %q", info, got, "dolphin")
	}
}

// TestNameCandidatesJetbrainsIdeaNotOverStripped pins that rule 4 is an
// allow-list, not a general "everything before the last dash" rule:
// "idea" is not a recognized generic suffix, so "jetbrains-idea" must
// NOT be reduced to "jetbrains" (which would collide with any other
// JetBrains product).
func TestNameCandidatesJetbrainsIdeaNotOverStripped(t *testing.T) {
	info := AppInfo{ResourceClass: "jetbrains-idea"}
	got := info.NameCandidates()
	if containsFold(got, "jetbrains") {
		t.Fatalf("NameCandidates(%+v) = %v, must not contain %q", info, got, "jetbrains")
	}
	if !containsFold(got, "jetbrains-idea") {
		t.Fatalf("NameCandidates(%+v) = %v, want it to contain %q", info, got, "jetbrains-idea")
	}
}

// TestNameCandidatesCaptionAloneIsEmpty pins that Caption is never used
// for matching — see AppInfo's doc comment.
func TestNameCandidatesCaptionAloneIsEmpty(t *testing.T) {
	info := AppInfo{Caption: "YouTube - Brave"}
	got := info.NameCandidates()
	if len(got) != 0 {
		t.Fatalf("NameCandidates(%+v) = %v, want empty (Caption must never be used)", info, got)
	}
}

func TestNameCandidatesBinaryContributesToken(t *testing.T) {
	info := AppInfo{ResourceClass: "brave-browser", Binary: "brave"}
	got := info.NameCandidates()
	if !containsFold(got, "brave") {
		t.Fatalf("NameCandidates(%+v) = %v, want it to contain %q", info, got, "brave")
	}
}

func TestDisplayName(t *testing.T) {
	cases := []struct {
		name string
		info AppInfo
		want string
	}{
		{"brave-browser resourceClass", AppInfo{ResourceClass: "brave-browser"}, "Brave"},
		{"google-chrome resourceClass", AppInfo{ResourceClass: "google-chrome"}, "Chrome"},
		{"plain resourceClass, no normalization", AppInfo{ResourceClass: "vesktop"}, "Vesktop"},
		{"desktopFileID reverse-DNS", AppInfo{DesktopFileID: "org.kde.dolphin.desktop"}, "Org.kde.dolphin"},
		{"caption alone yields nothing", AppInfo{Caption: "YouTube - Brave"}, ""},
		{"nothing at all", AppInfo{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.info.DisplayName(); got != tc.want {
				t.Fatalf("DisplayName(%+v) = %q, want %q", tc.info, got, tc.want)
			}
		})
	}
}
