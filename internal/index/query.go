package index

import (
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/sourcegraph/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

// Range is a resolved 0-based half-open source range.
type Range struct {
	StartLine, StartChar, EndLine, EndChar int32
}

// Location is a range within a document.
type Location struct {
	Path string
	Range
}

// OccurrenceRange normalises the three encodings SCIP uses for a range.
//
// Modern writers (scip-java 0.13.x, scip-typescript 0.4.x) emit the
// single_line_range / multi_line_range MESSAGES. The packed `range` int array is
// deprecated and comes back EMPTY, so a consumer that reads only `GetRange()`
// panics with an index-out-of-range on the very first occurrence.
//
// Any client of a SCIP index must handle all three. This is the single most
// common way to get a working-looking consumer that crashes on real data.
func OccurrenceRange(o *scip.Occurrence) (Range, bool) {
	if slr := o.GetSingleLineRange(); slr != nil {
		return Range{slr.GetLine(), slr.GetStartCharacter(), slr.GetLine(), slr.GetEndCharacter()}, true
	}
	if mlr := o.GetMultiLineRange(); mlr != nil {
		return Range{mlr.GetStartLine(), mlr.GetStartCharacter(), mlr.GetEndLine(), mlr.GetEndCharacter()}, true
	}
	switch r := o.GetRange(); len(r) {
	case 3:
		return Range{r[0], r[1], r[0], r[2]}, true
	case 4:
		return Range{r[0], r[1], r[2], r[3]}, true
	}
	return Range{}, false
}

func (r Range) Covers(line, char int32) bool {
	if line < r.StartLine || line > r.EndLine {
		return false
	}
	if line == r.StartLine && char < r.StartChar {
		return false
	}
	if line == r.EndLine && char > r.EndChar {
		return false
	}
	return true
}

// IsDefinition reports whether an occurrence carries the Definition role.
func IsDefinition(o *scip.Occurrence) bool {
	return o.GetSymbolRoles()&int32(scip.SymbolRole_Definition) != 0
}

// IsLocal reports whether a symbol is document-scoped rather than global.
// `local 0` recurs in every document, so these must never be used as join keys.
func IsLocal(symbol string) bool { return strings.HasPrefix(symbol, "local ") }

// Store is a loaded, queryable index.
type Store struct {
	mu sync.RWMutex

	index *scip.Index
	docs  map[string]*scip.Document // relative_path -> document

	defs map[string][]Location // symbol -> definition sites
	refs map[string][]Location // symbol -> reference sites
	info map[string]*scip.SymbolInformation
}

// Load reads and indexes a SCIP file.
func Load(path string) (*Store, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx scip.Index
	if err := proto.Unmarshal(raw, &idx); err != nil {
		return nil, err
	}

	s := &Store{
		index: &idx,
		docs:  make(map[string]*scip.Document, len(idx.Documents)),
		defs:  make(map[string][]Location),
		refs:  make(map[string][]Location),
		info:  make(map[string]*scip.SymbolInformation),
	}

	for _, doc := range idx.Documents {
		s.docs[doc.RelativePath] = doc
		for _, sym := range doc.Symbols {
			if !IsLocal(sym.Symbol) {
				s.info[sym.Symbol] = sym
			}
		}
		for _, occ := range doc.Occurrences {
			r, ok := OccurrenceRange(occ)
			if !ok {
				continue
			}
			// Local symbols are keyed per document to avoid `local 0` colliding
			// across every file in the project.
			key := occ.Symbol
			if IsLocal(key) {
				key = doc.RelativePath + "\x00" + key
			}
			loc := Location{Path: doc.RelativePath, Range: r}
			if IsDefinition(occ) {
				s.defs[key] = append(s.defs[key], loc)
			} else {
				s.refs[key] = append(s.refs[key], loc)
			}
		}
	}
	for _, sym := range idx.ExternalSymbols {
		s.info[sym.Symbol] = sym
	}
	return s, nil
}

func (s *Store) key(path, symbol string) string {
	if IsLocal(symbol) {
		return path + "\x00" + symbol
	}
	return symbol
}

// SymbolAt returns the symbol occurring at a position, innermost match first.
func (s *Store) SymbolAt(path string, line, char int32) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	doc, ok := s.docs[path]
	if !ok {
		return "", false
	}
	var best string
	var bestWidth int32 = 1 << 30
	for _, occ := range doc.Occurrences {
		r, ok := OccurrenceRange(occ)
		if !ok || !r.Covers(line, char) {
			continue
		}
		// Prefer the tightest enclosing occurrence.
		width := (r.EndLine-r.StartLine)*1024 + (r.EndChar - r.StartChar)
		if width < bestWidth {
			best, bestWidth = occ.Symbol, width
		}
	}
	return best, best != ""
}

// Definitions returns the definition sites for a symbol.
func (s *Store) Definitions(path, symbol string) []Location {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.defs[s.key(path, symbol)]
}

// References returns the reference sites for a symbol.
func (s *Store) References(path, symbol string, includeDefinition bool) []Location {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k := s.key(path, symbol)
	out := append([]Location(nil), s.refs[k]...)
	if includeDefinition {
		out = append(out, s.defs[k]...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].StartLine < out[j].StartLine
	})
	return out
}

// Document returns a document by relative path.
func (s *Store) Document(path string) (*scip.Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.docs[path]
	return d, ok
}

// SearchSymbols returns global symbols whose string contains query.
func (s *Store) SearchSymbols(query string, limit int) []*scip.SymbolInformation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query = strings.ToLower(query)
	var out []*scip.SymbolInformation
	for sym, si := range s.info {
		if query == "" || strings.Contains(strings.ToLower(sym), query) {
			out = append(out, si)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out
}

// Stats describes the loaded index.
func (s *Store) Stats() (documents, occurrences, symbols int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.index.Documents {
		occurrences += len(d.Occurrences)
	}
	return len(s.index.Documents), occurrences, len(s.info)
}
