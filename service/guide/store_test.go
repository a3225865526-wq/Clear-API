package guide

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func testBase() []byte {
	doc := Document{Pages: map[string]Article{"/a": {Title: "A", Source: "https://source.test/a", HTML: "<h1>A</h1>", Sections: []Section{{Title: "Group", Items: []Item{{Path: "/a", Title: "A"}, {Path: "/b", Title: "B"}}}}}, "/b": {Title: "B", Source: "https://source.test/b", HTML: "<h1>B</h1>"}}}
	raw, _ := json.Marshal(doc)
	return raw
}
func testChange(s *Store) Change {
	doc := s.Read()
	return Change{Revision: doc.Revision, Path: "/a", Title: "Edited", HTML: "<h1>Edited</h1><table><tr><td>Body</td></tr></table>", Next: "/b", Catalog: doc.Catalog}
}
func TestGuidePersistenceConflictAndHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(testBase(), dir)
	if err != nil {
		t.Fatal(err)
	}
	copy := s.Read()
	copy.Pages["/a"] = Article{Title: "tampered"}
	if s.Read().Pages["/a"].Title != "A" {
		t.Fatal("read leaked mutable state")
	}
	change := testChange(s)
	published, err := s.Save(change, 5)
	if err != nil {
		t.Fatal(err)
	}
	if published.Revision != 1 || published.Catalog[0].Items[0].Title != "Edited" {
		t.Fatal("title/revision not published")
	}
	if _, err = s.Save(change, 5); !errors.Is(err, ErrConflict) {
		t.Fatal("stale writer was not rejected")
	}
	restarted, err := NewStore(testBase(), dir)
	if err != nil || restarted.Read().Pages["/a"].HTML != change.HTML {
		t.Fatal("restart lost content", err)
	}
	history, err := restarted.Versions("/a")
	if err != nil || history[0].Article.Title != "A" {
		t.Fatal("original unavailable")
	}
	change.Revision = 1
	change.Title = history[0].Article.Title
	change.HTML = history[0].Article.HTML
	change.Catalog = history[0].Catalog
	if _, err = restarted.Save(change, 5); err != nil {
		t.Fatal(err)
	}
	if restarted.Read().Pages["/a"].Title != "A" || restarted.Read().Revision != 2 {
		t.Fatal("restore should publish new revision")
	}
}
func TestGuideRejectsUnsafeContentAndPaths(t *testing.T) {
	s, _ := NewStore(testBase(), t.TempDir())
	for _, body := range []string{`<script>alert(1)</script>`, `<img src="javascript:alert(1)">`, `<img onerror="x">`, `<iframe srcdoc="x">`, `<a href="java&#10;script:x">x</a>`, `<form><input></form>`} {
		c := testChange(s)
		c.HTML = body
		if _, err := s.Save(c, 5); !errors.Is(err, ErrInvalid) {
			t.Errorf("unsafe content accepted: %s", body)
		}
	}
	c := testChange(s)
	c.Path = "../../secret"
	if _, err := s.Save(c, 5); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown article accepted")
	}
	c = testChange(s)
	c.Catalog = []Section{{Title: "G", Items: []Item{{Path: "/missing", Title: "x"}}}}
	if _, err := s.Save(c, 5); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown directory article accepted")
	}
	if _, err := s.Image("../../secret"); !errors.Is(err, ErrInvalid) {
		t.Fatal("image traversal accepted")
	}
	if s.Read().Revision != 0 {
		t.Fatal("rejected change published")
	}
}
func TestGuideFailedWriteAndCorruptState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blocked")
	_ = os.WriteFile(dir, []byte("file"), 0600)
	s, _ := NewStore(testBase(), dir)
	// The initial read should fail when the parent is a file; create the store
	// before making its destination inaccessible to exercise publication failure.
	if s == nil {
		s, _ = NewStore(testBase(), t.TempDir())
		s.dir = dir
	}
	if _, err := s.Save(testChange(s), 5); err == nil {
		t.Fatal("expected write error")
	}
	if s.Read().Revision != 0 || s.Read().Pages["/a"].Title != "A" {
		t.Fatal("failed write changed live state")
	}
	corrupt := t.TempDir()
	_ = os.WriteFile(filepath.Join(corrupt, "published.json"), []byte("invalid JSON"), 0600)
	if _, err := NewStore(testBase(), corrupt); err == nil {
		t.Fatal("corrupt state silently ignored")
	}
}
func TestGuideConcurrentPublication(t *testing.T) {
	s, _ := NewStore(testBase(), t.TempDir())
	c := testChange(s)
	outcomes := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Save(c, 5); outcomes <- err }()
	}
	wg.Wait()
	close(outcomes)
	conflicts := 0
	for err := range outcomes {
		if errors.Is(err, ErrConflict) {
			conflicts++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if conflicts != 1 || s.Read().Revision != 1 {
		t.Fatal("racing writers overwrote each other")
	}
}
func TestGuideMigrationContentValid(t *testing.T) {
	raw, err := os.ReadFile("../../web/public/guide/pages.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc Document
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for path, page := range doc.Pages {
		if err = validateHTML(page.HTML); err != nil {
			t.Errorf("existing page cannot be saved: %s", path)
		}
	}
	if len(doc.Pages) != 32 {
		t.Fatal("migration baseline changed")
	}
	s, _ := NewStore(testBase(), t.TempDir())
	name := strings.Repeat("a", 64) + ".png"
	if err := s.SaveImage([]byte("bytes"), name); err != nil {
		t.Fatal(err)
	}
	file, err := s.Image(name)
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
}
