package limiter

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func repoPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "rules.json")
}

func TestFileRepositoryLoadMissingFile(t *testing.T) {
	repo := NewFileRepository(repoPath(t))

	rules, err := repo.Load()
	if err != nil {
		t.Fatalf("Load of a missing file returned %v, want nil", err)
	}
	if rules != nil {
		t.Fatalf("Load returned %+v, want nil", rules)
	}
}

func TestFileRepositoryRoundTrip(t *testing.T) {
	repo := NewFileRepository(repoPath(t))
	want := []Rule{
		rule("posts", "token_bucket", 50),
		rule("logins", "fixed_window", 5),
	}

	if err := repo.Save(want); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	got, err := repo.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Load returned %d rules, want %d", len(got), len(want))
	}

	byName := map[string]Rule{}
	for _, r := range got {
		byName[r.Name] = r
	}
	for _, r := range want {
		if !reflect.DeepEqual(byName[r.Name], r) {
			t.Fatalf("round trip changed %s: got %+v, want %+v", r.Name, byName[r.Name], r)
		}
	}
}

func TestFileRepositorySavesSortedByName(t *testing.T) {
	path := repoPath(t)
	repo := NewFileRepository(path)

	if err := repo.Save([]Rule{
		rule("zulu", "fixed_window", 1),
		rule("alpha", "fixed_window", 1),
		rule("mike", "fixed_window", 1),
	}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := repo.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	for i, want := range []string{"alpha", "mike", "zulu"} {
		if got[i].Name != want {
			t.Fatalf("rule %d = %s, want %s; file is not sorted", i, got[i].Name, want)
		}
	}
}

func TestFileRepositorySaveOverwrites(t *testing.T) {
	repo := NewFileRepository(repoPath(t))

	if err := repo.Save([]Rule{rule("a", "fixed_window", 1), rule("b", "fixed_window", 1)}); err != nil {
		t.Fatalf("first Save failed: %v", err)
	}
	if err := repo.Save([]Rule{rule("a", "token_bucket", 9)}); err != nil {
		t.Fatalf("second Save failed: %v", err)
	}

	got, err := repo.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(got) != 1 || got[0].Name != "a" || got[0].Algorithm != "token_bucket" {
		t.Fatalf("Load = %+v, want only the rewritten rule", got)
	}
}

func TestFileRepositorySaveEmptyList(t *testing.T) {
	repo := NewFileRepository(repoPath(t))

	if err := repo.Save([]Rule{}); err != nil {
		t.Fatalf("Save of an empty list failed: %v", err)
	}
	got, err := repo.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Load = %+v, want empty", got)
	}
}

func TestFileRepositoryLoadCorruptJSON(t *testing.T) {
	path := repoPath(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("writing the corrupt file failed: %v", err)
	}

	if _, err := NewFileRepository(path).Load(); err == nil {
		t.Fatal("Load of corrupt JSON returned nil error")
	}
}

func TestFileRepositoryLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	repo := NewFileRepository(filepath.Join(dir, "rules.json"))

	for i := 0; i < 5; i++ {
		if err := repo.Save([]Rule{rule("a", "fixed_window", i)}); err != nil {
			t.Fatalf("Save %d failed: %v", i, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "rules.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("directory holds %v, want only rules.json; temp files leaked", names)
	}
}

func TestFileRepositorySaveToUnwritableDirectory(t *testing.T) {
	repo := NewFileRepository(filepath.Join(t.TempDir(), "missing-dir", "rules.json"))

	if err := repo.Save([]Rule{rule("a", "fixed_window", 1)}); err == nil {
		t.Fatal("Save into a missing directory returned nil error")
	}
}

func TestFileRepositoryFeedsRuleStore(t *testing.T) {
	path := repoPath(t)
	store := NewRuleStore(NewFileRepository(path))

	if err := store.Set(rule("posts", "token_bucket", 50)); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	reloaded := NewRuleStore(NewFileRepository(path))
	count, err := reloaded.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("Load returned %d, want 1", count)
	}
	got, found := reloaded.Get("posts")
	if !found {
		t.Fatal("the rule did not survive a save and reload")
	}
	if got.Algorithm != "token_bucket" || got.Limit != 50 {
		t.Fatalf("reloaded rule = %+v", got)
	}
}
