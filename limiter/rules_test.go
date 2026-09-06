package limiter

import (
	"errors"
	"sync"
	"testing"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
)

var errSaveFailed = errors.New("save failed")

type fakeRepository struct {
	mu       sync.Mutex
	loaded   []Rule
	saved    [][]Rule
	loadErr  error
	saveErr  error
	failNext bool
}

func (r *fakeRepository) Load() ([]Rule, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	return r.loaded, nil
}

func (r *fakeRepository) Save(rules []Rule) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNext {
		r.failNext = false
		return errSaveFailed
	}
	if r.saveErr != nil {
		return r.saveErr
	}
	snapshot := make([]Rule, len(rules))
	copy(snapshot, rules)
	r.saved = append(r.saved, snapshot)
	return nil
}

func (r *fakeRepository) lastSaved() []Rule {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.saved) == 0 {
		return nil
	}
	return r.saved[len(r.saved)-1]
}

func TestRuleStoreSetGet(t *testing.T) {
	store := NewRuleStore(&fakeRepository{})
	want := rule("posts", "token_bucket", 50)

	if err := store.Set(want); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	got, found := store.Get("posts")
	if !found {
		t.Fatal("Get did not find the rule just set")
	}
	if got != want {
		t.Fatalf("Get = %+v, want %+v", got, want)
	}
	if _, found := store.Get("missing"); found {
		t.Fatal("Get found a rule that was never set")
	}
}

func TestRuleStoreSetOverwrites(t *testing.T) {
	store := NewRuleStore(&fakeRepository{})
	store.Set(rule("posts", "fixed_window", 10))
	store.Set(rule("posts", "token_bucket", 99))

	got, _ := store.Get("posts")
	if got.Algorithm != "token_bucket" || got.Limit != 99 {
		t.Fatalf("Get = %+v, want the overwriting rule", got)
	}
	if len(store.List()) != 1 {
		t.Fatalf("List has %d rules, want 1", len(store.List()))
	}
}

func TestRuleStoreSetRollsBackNewRuleOnSaveFailure(t *testing.T) {
	repo := &fakeRepository{saveErr: errSaveFailed}
	store := NewRuleStore(repo)

	err := store.Set(rule("posts", "fixed_window", 10))
	if !errors.Is(err, errSaveFailed) {
		t.Fatalf("Set error = %v, want errSaveFailed", err)
	}
	if _, found := store.Get("posts"); found {
		t.Fatal("rule survived in memory after the save failed")
	}
	if len(store.List()) != 0 {
		t.Fatalf("List has %d rules, want 0", len(store.List()))
	}
}

func TestRuleStoreSetRestoresPreviousRuleOnSaveFailure(t *testing.T) {
	repo := &fakeRepository{}
	store := NewRuleStore(repo)
	original := rule("posts", "fixed_window", 10)
	if err := store.Set(original); err != nil {
		t.Fatalf("initial Set failed: %v", err)
	}

	repo.failNext = true
	if err := store.Set(rule("posts", "token_bucket", 99)); !errors.Is(err, errSaveFailed) {
		t.Fatalf("Set error = %v, want errSaveFailed", err)
	}

	got, found := store.Get("posts")
	if !found {
		t.Fatal("the previous rule was dropped instead of restored")
	}
	if got != original {
		t.Fatalf("Get = %+v, want the original %+v", got, original)
	}
}

func TestRuleStoreDelete(t *testing.T) {
	store := NewRuleStore(&fakeRepository{})
	store.Set(rule("posts", "fixed_window", 10))

	if err := store.Delete("posts"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, found := store.Get("posts"); found {
		t.Fatal("rule survived the delete")
	}
}

func TestRuleStoreDeleteMissingIsNoOp(t *testing.T) {
	repo := &fakeRepository{saveErr: errSaveFailed}
	store := NewRuleStore(repo)

	if err := store.Delete("missing"); err != nil {
		t.Fatalf("Delete of a missing rule returned %v, want nil and no save", err)
	}
}

func TestRuleStoreDeleteRestoresOnSaveFailure(t *testing.T) {
	repo := &fakeRepository{}
	store := NewRuleStore(repo)
	original := rule("posts", "fixed_window", 10)
	store.Set(original)

	repo.failNext = true
	if err := store.Delete("posts"); !errors.Is(err, errSaveFailed) {
		t.Fatalf("Delete error = %v, want errSaveFailed", err)
	}

	got, found := store.Get("posts")
	if !found {
		t.Fatal("rule was lost after the delete failed to persist")
	}
	if got != original {
		t.Fatalf("Get = %+v, want the original %+v", got, original)
	}
}

func TestRuleStoreLoadMergesIntoExisting(t *testing.T) {
	repo := &fakeRepository{loaded: []Rule{
		rule("posts", "token_bucket", 50),
		rule("logins", "fixed_window", 5),
	}}
	store := NewRuleStore(repo)
	store.Set(rule("uploads", "sliding_window", 3))

	count, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("Load returned %d, want 2", count)
	}
	if len(store.List()) != 3 {
		t.Fatalf("List has %d rules, want 3; Load replaced instead of merging", len(store.List()))
	}
	if _, found := store.Get("uploads"); !found {
		t.Fatal("Load dropped a rule that was set in memory")
	}
}

func TestRuleStoreLoadPropagatesError(t *testing.T) {
	store := NewRuleStore(&fakeRepository{loadErr: errSaveFailed})
	if _, err := store.Load(); !errors.Is(err, errSaveFailed) {
		t.Fatalf("Load error = %v, want errSaveFailed", err)
	}
}

func TestRuleStoreListSnapshotIsIndependent(t *testing.T) {
	store := NewRuleStore(&fakeRepository{})
	store.Set(rule("posts", "fixed_window", 10))

	list := store.List()
	list[0].Limit = 999

	got, _ := store.Get("posts")
	if got.Limit != 10 {
		t.Fatalf("mutating the List result changed the store: limit = %d", got.Limit)
	}
}

func TestRuleStoreSavesEveryRule(t *testing.T) {
	repo := &fakeRepository{}
	store := NewRuleStore(repo)
	store.Set(rule("posts", "fixed_window", 10))
	store.Set(rule("logins", "token_bucket", 5))

	if got := len(repo.lastSaved()); got != 2 {
		t.Fatalf("saved %d rules, want the full set of 2", got)
	}
}

func TestRuleStoreCountByAlgorithm(t *testing.T) {
	store := NewRuleStore(&fakeRepository{})
	store.Set(rule("a", "fixed_window", 1))
	store.Set(rule("b", "fixed_window", 1))
	store.Set(rule("c", "token_bucket", 1))

	counts := store.CountByAlgorithm()
	if counts["fixed_window"] != 2 {
		t.Fatalf("fixed_window count = %d, want 2", counts["fixed_window"])
	}
	if counts["token_bucket"] != 1 {
		t.Fatalf("token_bucket count = %d, want 1", counts["token_bucket"])
	}
	if _, found := counts["sliding_window"]; found {
		t.Fatal("counts include an algorithm with no rules")
	}

	store.Delete("a")
	if counts := store.CountByAlgorithm(); counts["fixed_window"] != 1 {
		t.Fatalf("fixed_window count after delete = %d, want 1", counts["fixed_window"])
	}
}

func TestRuleStoreConcurrentAccess(t *testing.T) {
	store := NewRuleStore(&fakeRepository{})
	names := []string{"a", "b", "c", "d"}
	for _, name := range names {
		store.Set(rule(name, "fixed_window", 1))
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := names[i%len(names)]
			for j := 0; j < 50; j++ {
				store.Set(rule(name, "token_bucket", j))
				store.Get(name)
				store.List()
				store.CountByAlgorithm()
			}
		}(i)
	}
	wg.Wait()

	if len(store.List()) != len(names) {
		t.Fatalf("List has %d rules, want %d", len(store.List()), len(names))
	}
}

func TestRuleEmbedsAlgorithmRule(t *testing.T) {
	r := Rule{
		Rule:      algorithms.Rule{Name: "posts", Limit: 50, WindowSec: 86400},
		Algorithm: "token_bucket",
	}
	if r.Name != "posts" || r.Limit != 50 || r.WindowSec != 86400 {
		t.Fatalf("embedded fields not promoted: %+v", r)
	}
}
