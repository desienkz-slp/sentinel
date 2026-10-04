package learning

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRecipeRemovePersistsWithoutDeadlock(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(fmt.Sprint(persistent), func(t *testing.T) {
			path := ""
			if persistent {
				path = filepath.Join(t.TempDir(), "recipes.json")
			}
			s := NewRecipeStore(path)
			s.Record(SigDNS, []string{"dns"}, "SEHAT")
			s.Record(SigDNS, []string{"ping"}, "SEHAT")
			done := make(chan error, 1)
			go func() { done <- s.Remove(SigDNS, []string{"dns"}) }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Remove deadlocked")
			}
			if got := s.BestFor(SigDNS); len(got) != 1 || got[0] != "ping" {
				t.Fatalf("remaining recipe: %v", got)
			}
			if persistent {
				loaded := NewRecipeStore(path)
				if got := loaded.BestFor(SigDNS); len(got) != 1 || got[0] != "ping" {
					t.Fatalf("persisted recipe: %v", got)
				}
			}
			if err := s.Remove(SigDNS, []string{"ping"}); err != nil {
				t.Fatal(err)
			}
			if s.Stats()["signature"].(int) != 0 {
				t.Fatal("empty signature retained")
			}
			if persistent && len(NewRecipeStore(path).Semua()) != 0 {
				t.Fatal("last removal not persisted")
			}
			if err := s.Remove(SigDNS, []string{"missing"}); err == nil {
				t.Fatal("missing recipe removed successfully")
			}
		})
	}
}

func TestRecipeSaveFailureRemainsDirty(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewRecipeStore(filepath.Join(blocker, "recipes.json"))
	s.Record(SigDNS, []string{"dns"}, "SEHAT")
	if err := s.SaveIfDirty(); err == nil {
		t.Fatal("save should fail")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIfDirty(); err != nil {
		t.Fatal(err)
	}
	if len(NewRecipeStore(s.path).BestFor(SigDNS)) != 1 {
		t.Fatal("failed save discarded dirty state; retry did not persist recipe")
	}
}

func TestRecipeConcurrentSaveAndRecord(t *testing.T) {
	s := NewRecipeStore(filepath.Join(t.TempDir(), "recipes.json"))
	const workers, iterations = 4, 30
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				s.Record(SigDNS, []string{"dns"}, "SEHAT")
				if err := s.Save(); err != nil {
					t.Errorf("concurrent Save: %v", err)
					return
				}
				s.BestFor(SigDNS)
				s.Semua()
			}
		}()
	}
	wg.Wait()
	if err := s.SaveIfDirty(); err != nil {
		t.Fatal(err)
	}
	loaded := NewRecipeStore(s.path).Semua()
	if len(loaded) != 1 || loaded[0]["hits"].(int) != workers*iterations {
		t.Fatalf("lost records: %v", loaded)
	}
}

func TestRecipeSemuaReturnsOwnedTools(t *testing.T) {
	s := NewRecipeStore("")
	s.Record(SigDNS, []string{"dns"}, "SEHAT")
	s.Semua()[0]["tools"].([]string)[0] = "changed"
	if got := s.BestFor(SigDNS); got[0] != "dns" {
		t.Fatalf("caller mutated store: %v", got)
	}
}
