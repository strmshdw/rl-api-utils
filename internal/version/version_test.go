package version

import "testing"

func TestVersionDisplayAndMode(t *testing.T) {
	origVer, origCommit, origMode := Version, Commit, BuildMode
	defer func() {
		Version, Commit, BuildMode = origVer, origCommit, origMode
	}()

	Version = "2.1.0"
	BuildMode = "production"
	if IsDev() {
		t.Fatal("expected production mode not to report IsDev")
	}
	if got := Display(); got != "v2.1.0" {
		t.Fatalf("Display() = %q, want v2.1.0", got)
	}

	BuildMode = "development"
	Commit = "abc1234"
	if !IsDev() {
		t.Fatal("expected development mode to report IsDev")
	}
	if got := Display(); got != "v2.1.0-dev (abc1234)" {
		t.Fatalf("Display() = %q, want v2.1.0-dev (abc1234)", got)
	}
}
