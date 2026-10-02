// Package guide stores published guide edits independently of business data.
package guide

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

var ErrConflict = errors.New("guide revision conflict")
var ErrInvalid = errors.New("invalid guide content")
var imageName = regexp.MustCompile(`^[a-f0-9]{64}\.(png|jpeg|gif)$`)

type Item struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}
type Section struct {
	Title string `json:"title"`
	Items []Item `json:"items"`
}
type Article struct {
	Title    string    `json:"title"`
	Source   string    `json:"source"`
	HTML     string    `json:"html"`
	Previous string    `json:"previous,omitempty"`
	Next     string    `json:"next,omitempty"`
	Sections []Section `json:"sections"`
}
type Document struct {
	Pages        map[string]Article `json:"pages"`
	Catalog      []Section          `json:"catalog"`
	Revision     int64              `json:"revision"`
	Inaccessible map[string]string  `json:"inaccessible"`
}
type Version struct {
	Revision int64     `json:"revision"`
	Path     string    `json:"path"`
	At       int64     `json:"at"`
	Author   int       `json:"author"`
	Article  Article   `json:"article"`
	Catalog  []Section `json:"catalog"`
}
type state struct {
	Document Document  `json:"document"`
	History  []Version `json:"history"`
}
type Change struct {
	Revision int64     `json:"revision"`
	Path     string    `json:"path"`
	Title    string    `json:"title"`
	HTML     string    `json:"html"`
	Previous string    `json:"previous"`
	Next     string    `json:"next"`
	Catalog  []Section `json:"catalog"`
}
type Store struct {
	mu      sync.Mutex
	dir     string
	base    Document
	current state
}

func NewStore(base []byte, dir string) (*Store, error) {
	s := &Store{dir: dir}
	if err := json.Unmarshal(base, &s.base); err != nil {
		return nil, err
	}
	if len(s.base.Pages) == 0 {
		return nil, ErrInvalid
	}
	if home, ok := s.base.Pages["/start/register"]; ok {
		s.base.Catalog = home.Sections
	}
	if len(s.base.Catalog) == 0 {
		for _, p := range s.base.Pages {
			if len(p.Sections) > 0 {
				s.base.Catalog = p.Sections
				break
			}
		}
	}
	s.current.Document = s.base
	raw, err := os.ReadFile(filepath.Join(dir, "published.json"))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	s.current = state{}
	if err = json.Unmarshal(raw, &s.current); err != nil {
		return nil, err
	}
	if len(s.current.Document.Pages) != len(s.base.Pages) {
		return nil, ErrInvalid
	}
	for path, p := range s.current.Document.Pages {
		if _, ok := s.base.Pages[path]; !ok || p.Source != s.base.Pages[path].Source {
			return nil, ErrInvalid
		}
	}
	return s, nil
}

// Return a deep copy: callers cannot mutate live or baseline content.
func (s *Store) Read() Document {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.current.Document)
	var doc Document
	_ = json.Unmarshal(raw, &doc)
	return doc
}

func (s *Store) Versions(path string) ([]Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	base, ok := s.base.Pages[path]
	if !ok {
		return nil, ErrInvalid
	}
	versions := []Version{{Path: path, Article: base, Catalog: s.base.Catalog}}
	for i := len(s.current.History) - 1; i >= 0; i-- {
		if s.current.History[i].Path == path {
			versions = append(versions, s.current.History[i])
		}
	}
	raw, _ := json.Marshal(versions)
	_ = json.Unmarshal(raw, &versions)
	return versions, nil
}

func validateHTML(value string) error {
	if len(value) == 0 || len(value) > 1024*1024 {
		return ErrInvalid
	}
	root, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return ErrInvalid
	}
	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "iframe", "object", "embed", "base", "link", "meta", "style", "form":
				return ErrInvalid
			}
			for _, a := range n.Attr {
				if strings.HasPrefix(a.Key, "on") || a.Key == "srcdoc" {
					return ErrInvalid
				}
				if a.Key == "href" || a.Key == "src" || a.Key == "action" {
					value := strings.TrimSpace(a.Val)
					compact := strings.Map(func(r rune) rune {
						if r <= 32 {
							return -1
						}
						return r
					}, strings.ToLower(value))
					if strings.HasPrefix(compact, "javascript:") || strings.HasPrefix(compact, "vbscript:") || strings.HasPrefix(compact, "data:") {
						return ErrInvalid
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}

func (s *Store) Save(change Change, author int) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if change.Revision != s.current.Document.Revision {
		return Document{}, ErrConflict
	}
	old, ok := s.current.Document.Pages[change.Path]
	if !ok || len(strings.TrimSpace(change.Title)) == 0 || len(change.Title) > 200 {
		return Document{}, ErrInvalid
	}
	if err := validateHTML(change.HTML); err != nil {
		return Document{}, err
	}
	for _, related := range []string{change.Previous, change.Next} {
		if related != "" {
			if _, ok := s.current.Document.Pages[related]; !ok || related == change.Path {
				return Document{}, ErrInvalid
			}
		}
	}
	if len(change.Catalog) > 32 {
		return Document{}, ErrInvalid
	}
	count := 0
	seen := map[string]bool{}
	for _, group := range change.Catalog {
		if len(strings.TrimSpace(group.Title)) == 0 || len(group.Title) > 200 {
			return Document{}, ErrInvalid
		}
		for _, item := range group.Items {
			count++
			if _, ok := s.current.Document.Pages[item.Path]; !ok || seen[item.Path] || len(item.Title) == 0 || len(item.Title) > 200 {
				return Document{}, ErrInvalid
			}
			seen[item.Path] = true
		}
	}
	if count > 200 {
		return Document{}, ErrInvalid
	}
	// Build a candidate without mutating the last successfully published state.
	raw, _ := json.Marshal(s.current)
	var candidate state
	_ = json.Unmarshal(raw, &candidate)
	candidate.History = append(candidate.History, Version{Revision: candidate.Document.Revision, Path: change.Path, At: time.Now().Unix(), Author: author, Article: old, Catalog: candidate.Document.Catalog})
	if len(candidate.History) > 20 {
		candidate.History = candidate.History[len(candidate.History)-20:]
	}
	old.Title, old.HTML, old.Previous, old.Next = change.Title, change.HTML, change.Previous, change.Next
	candidate.Document.Pages[change.Path] = old
	candidate.Document.Catalog = change.Catalog
	for i := range candidate.Document.Catalog {
		for j := range candidate.Document.Catalog[i].Items {
			if candidate.Document.Catalog[i].Items[j].Path == change.Path {
				candidate.Document.Catalog[i].Items[j].Title = change.Title
			}
		}
	}
	candidate.Document.Revision++
	raw, err := json.Marshal(candidate)
	if err != nil {
		return Document{}, err
	}
	if err = os.MkdirAll(s.dir, 0700); err != nil {
		return Document{}, err
	}
	file, err := os.CreateTemp(s.dir, ".guide-*")
	if err != nil {
		return Document{}, err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return Document{}, err
	}
	if err = os.Rename(temp, filepath.Join(s.dir, "published.json")); err != nil {
		return Document{}, err
	}
	s.current = candidate
	raw, _ = json.Marshal(candidate.Document)
	var result Document
	_ = json.Unmarshal(raw, &result)
	return result, nil
}

func (s *Store) SaveImage(data []byte, name string) error {
	if !imageName.MatchString(name) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.dir, "images")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	target := filepath.Join(dir, name)
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	file, err := os.CreateTemp(dir, ".image-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temp, target)
}
func (s *Store) Image(name string) (io.ReadCloser, error) {
	if !imageName.MatchString(name) {
		return nil, fmt.Errorf("%w: image name", ErrInvalid)
	}
	return os.Open(filepath.Join(s.dir, "images", name))
}
