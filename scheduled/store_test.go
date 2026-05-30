package scheduled

import (
	"sort"
	"testing"
	"time"
)

func TestMemStore_SaveAndLoad(t *testing.T) {
	s := newMemStore()

	now := time.Date(2025, 3, 15, 10, 0, 0, 0, time.UTC)
	status := TaskStatus{
		Name:           "my-task",
		Schedule:       "every 5m",
		LastRunAt:      now,
		LastSuccessAt:  now,
		NextDueAt:      now.Add(5 * time.Minute),
		CurrentAttempt: 1,
		RecentError:    "",
	}

	if err := s.Save(status); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, ok := s.Load("my-task")
	if !ok {
		t.Fatal("Load: expected ok=true")
	}
	if loaded.Name != status.Name {
		t.Errorf("Name: got %q, want %q", loaded.Name, status.Name)
	}
	if loaded.Schedule != status.Schedule {
		t.Errorf("Schedule: got %q, want %q", loaded.Schedule, status.Schedule)
	}
	if !loaded.LastRunAt.Equal(status.LastRunAt) {
		t.Errorf("LastRunAt: got %v, want %v", loaded.LastRunAt, status.LastRunAt)
	}
	if !loaded.NextDueAt.Equal(status.NextDueAt) {
		t.Errorf("NextDueAt: got %v, want %v", loaded.NextDueAt, status.NextDueAt)
	}
	if loaded.CurrentAttempt != status.CurrentAttempt {
		t.Errorf("CurrentAttempt: got %d, want %d", loaded.CurrentAttempt, status.CurrentAttempt)
	}
}

func TestMemStore_LoadUnknown(t *testing.T) {
	s := newMemStore()
	_, ok := s.Load("does-not-exist")
	if ok {
		t.Error("Load of unknown key: expected ok=false")
	}
}

func TestMemStore_SaveUpdates(t *testing.T) {
	s := newMemStore()

	s.Save(TaskStatus{Name: "t", CurrentAttempt: 1, RecentError: "oops"})
	s.Save(TaskStatus{Name: "t", CurrentAttempt: 2, RecentError: ""})

	loaded, ok := s.Load("t")
	if !ok {
		t.Fatal("Load after update: expected ok=true")
	}
	if loaded.CurrentAttempt != 2 {
		t.Errorf("CurrentAttempt after update: got %d, want 2", loaded.CurrentAttempt)
	}
	if loaded.RecentError != "" {
		t.Errorf("RecentError after update: got %q, want empty", loaded.RecentError)
	}
}

func TestMemStore_List(t *testing.T) {
	s := newMemStore()

	names := []string{"alpha", "beta", "gamma"}
	for _, n := range names {
		s.Save(TaskStatus{Name: n})
	}

	list := s.List()
	if len(list) != 3 {
		t.Fatalf("List: got %d records, want 3", len(list))
	}

	// Sort for deterministic comparison
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	for i, want := range names {
		if list[i].Name != want {
			t.Errorf("List[%d].Name: got %q, want %q", i, list[i].Name, want)
		}
	}
}

func TestMemStore_ListEmpty(t *testing.T) {
	s := newMemStore()
	list := s.List()
	if len(list) != 0 {
		t.Errorf("List on empty store: got %d, want 0", len(list))
	}
}

func TestMemStore_ListReturnsSnapshot(t *testing.T) {
	s := newMemStore()
	s.Save(TaskStatus{Name: "a"})

	list := s.List()
	// Mutating the returned slice should not affect the store
	list[0].Name = "mutated"

	_, ok := s.Load("a")
	if !ok {
		t.Error("original record disappeared after slice mutation")
	}
}

func TestMemStore_ConcurrentAccess(t *testing.T) {
	s := newMemStore()
	done := make(chan struct{})

	// Writer
	go func() {
		for i := 0; i < 100; i++ {
			s.Save(TaskStatus{Name: "concurrent-task", CurrentAttempt: i})
		}
		close(done)
	}()

	// Reader (concurrent)
	for i := 0; i < 50; i++ {
		s.Load("concurrent-task")
		s.List()
	}

	<-done
}
