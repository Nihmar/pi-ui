// Package search answers "where is this text": in the files of a workspace
// (ripgrep) and in the conversations pi wrote to its own session files (a JSONL
// scan, because those files are not a workspace the operator configured).
//
// The two halves share the confinement rule: a file hit can only come from inside
// a root, and a session hit can only come from a session directory the server was
// told about.
package search

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// Defaults bound one query.
const (
	DefaultRG         = "rg"
	DefaultTimeout    = 20 * time.Second
	DefaultMaxResults = 200
	// maxSessionBytes skips a conversation file that grew past this: a search must
	// not read a gigabyte to answer one query.
	maxSessionBytes = 32 << 20
	// maxLineBytes bounds one ripgrep line, so a minified file cannot blow up memory.
	maxLineBytes = 1 << 20
)

// Config wires the service.
type Config struct {
	// FS is the confinement for the file half (required).
	FS *fs.Service
	// SessionDirs are the directories pi writes its session JSONL into. They are
	// allowed on their own: they live outside the workspaces by design.
	SessionDirs []string
	// RG is the ripgrep binary (default DefaultRG).
	RG string
	// Timeout bounds ripgrep (default DefaultTimeout).
	Timeout time.Duration
}

// Service is the search seam.
type Service struct {
	fs          *fs.Service
	sessionDirs []string
	rg          string
	timeout     time.Duration
}

// New builds the service.
func New(cfg Config) (*Service, error) {
	if cfg.FS == nil {
		return nil, errors.New("search: the filesystem service is required")
	}
	service := &Service{
		fs:      cfg.FS,
		rg:      cfg.RG,
		timeout: cfg.Timeout,
	}
	if service.rg == "" {
		service.rg = DefaultRG
	}
	if service.timeout <= 0 {
		service.timeout = DefaultTimeout
	}
	for _, dir := range cfg.SessionDirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, err
		}
		service.sessionDirs = append(service.sessionDirs, abs)
	}
	return service, nil
}

// Hit is one match, wherever it came from.
type Hit struct {
	// Kind is "file" or "message".
	Kind string `json:"kind"`
	// Path is the file the match is in.
	Path string `json:"path"`
	// Rel is the path relative to the workspace root, for file hits.
	Rel string `json:"rel,omitempty"`
	// RootID names the workspace a file hit belongs to.
	RootID string `json:"rootId,omitempty"`
	// Line and Column locate the match inside the file (1-based).
	Line   int `json:"line,omitempty"`
	Column int `json:"column,omitempty"`
	// Text is the matching line, or the message excerpt.
	Text string `json:"text"`
	// SessionID, Role and At describe a message hit.
	SessionID string `json:"sessionId,omitempty"`
	Role      string `json:"role,omitempty"`
	At        string `json:"at,omitempty"`
}

// Query is one search request.
type Query struct {
	// Text is what to look for.
	Text string
	// Scope selects the halves: "files", "messages" or both (default both).
	Scope []string
	// Dir bounds the file half to one workspace directory.
	Dir string
	// CaseSensitive makes the match exact.
	CaseSensitive bool
	// Limit bounds the number of hits (default DefaultMaxResults).
	Limit int
}

// wants reports whether one half is in scope.
func (q Query) wants(scope string) bool {
	if len(q.Scope) == 0 {
		return true
	}
	for _, candidate := range q.Scope {
		if strings.EqualFold(strings.TrimSpace(candidate), scope) {
			return true
		}
	}
	return false
}

// Search runs the query and returns the hits, files first.
func (s *Service) Search(ctx context.Context, query Query) ([]Hit, error) {
	text := strings.TrimSpace(query.Text)
	if len(text) < 2 {
		return nil, sessions.Codedf(sessions.CodeBadRequest, "a search needs at least two characters")
	}
	limit := query.Limit
	if limit <= 0 || limit > DefaultMaxResults*5 {
		limit = DefaultMaxResults
	}
	hits := make([]Hit, 0, 16)
	if query.wants("files") {
		found, err := s.searchFiles(ctx, query, text, limit)
		if err != nil {
			return nil, err
		}
		hits = append(hits, found...)
	}
	if query.wants("messages") {
		found, err := s.searchSessions(query, text, limit-len(hits))
		if err != nil {
			return nil, err
		}
		hits = append(hits, found...)
	}
	return hits, nil
}

// searchFiles runs ripgrep inside one workspace directory.
func (s *Service) searchFiles(ctx context.Context, query Query, text string, limit int) ([]Hit, error) {
	dir, err := s.fs.Resolve(query.Dir)
	if err != nil {
		return nil, err
	}
	if _, lookErr := exec.LookPath(s.rg); lookErr != nil {
		return nil, sessions.Codedf(sessions.CodeUnsupported,
			"the file search needs ripgrep on the host")
	}
	args := []string{
		"--json", "--line-number", "--max-columns", "400", "--max-count", "20",
		"--no-heading", "--color", "never",
	}
	if !query.CaseSensitive {
		args = append(args, "--ignore-case")
	}
	args = append(args, "--", text, dir)

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	command := exec.CommandContext(ctx, s.rg, args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, sessions.Codedf(sessions.CodeInternal, "ripgrep cannot be started: %v", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, sessions.Codedf(sessions.CodeInternal, "ripgrep cannot be started: %v", err)
	}

	hits := make([]Hit, 0, 16)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	for scanner.Scan() {
		hit, ok := parseRipgrepLine(scanner.Bytes())
		if !ok {
			continue
		}
		if rel, rootID, ok := s.relOf(hit.Path); ok {
			hit.Rel = rel
			hit.RootID = rootID
			hits = append(hits, hit)
		}
		if len(hits) >= limit {
			break
		}
	}
	scanErr := scanner.Err()
	_ = command.Process.Kill()
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return nil, sessions.Codedf(sessions.CodeTimeout, "the file search timed out")
	}
	if scanErr != nil {
		return nil, sessions.Codedf(sessions.CodeInternal, "ripgrep output is unreadable: %v", scanErr)
	}
	// Exit status 1 is ripgrep's "no match", not a failure.
	if waitErr != nil && len(hits) == 0 && !isNoMatch(waitErr) {
		message := strings.TrimSpace(stderr.String())
		return nil, sessions.Codedf(sessions.CodeBadRequest, "ripgrep: %s", message)
	}
	return hits, nil
}

// isNoMatch reports ripgrep's exit status 1.
func isNoMatch(err error) bool {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode() == 1
	}
	return false
}

// relOf places one hit inside the workspaces, using the root ids the client knows.
func (s *Service) relOf(path string) (string, string, bool) {
	for _, root := range s.fs.Roots() {
		if path == root.Path {
			return ".", root.ID, true
		}
		prefix := root.Path + string(filepath.Separator)
		if strings.HasPrefix(path, prefix) {
			return strings.TrimPrefix(path, prefix), root.ID, true
		}
	}
	return "", "", false
}

// ripgrepLine is the little of ripgrep's JSON this service needs.
type ripgrepLine struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
	} `json:"data"`
}

// parseRipgrepLine turns one JSON line into a hit; not every line is a match.
func parseRipgrepLine(line []byte) (Hit, bool) {
	var decoded ripgrepLine
	if err := json.Unmarshal(line, &decoded); err != nil {
		return Hit{}, false
	}
	if decoded.Type != "match" || decoded.Data.Path.Text == "" {
		return Hit{}, false
	}
	column := 0
	if len(decoded.Data.Submatches) > 0 {
		column = decoded.Data.Submatches[0].Start + 1
	}
	return Hit{
		Kind:   "file",
		Path:   decoded.Data.Path.Text,
		Line:   decoded.Data.LineNumber,
		Column: column,
		Text:   strings.TrimRight(decoded.Data.Lines.Text, "\r\n"),
	}, true
}

// searchSessions scans the session JSONL files for a substring.
//
// It is a scan and not an index on purpose: pi owns those files, they are append-only
// and usually small, and an index would be a second copy of conversations to keep in
// sync (`AGENTS.md`: conversations live in pi's own session files).
func (s *Service) searchSessions(query Query, text string, limit int) ([]Hit, error) {
	if limit <= 0 {
		return nil, nil
	}
	hits := make([]Hit, 0, 8)
	for _, dir := range s.sessionDirs {
		if len(hits) >= limit {
			break
		}
		files, err := sessionFiles(dir)
		if err != nil {
			continue
		}
		for _, file := range files {
			if len(hits) >= limit {
				break
			}
			hits = append(hits, scanSessionFile(file, text, query.CaseSensitive, limit-len(hits))...)
		}
	}
	return hits, nil
}

// sessionFiles lists the JSONL files of one session directory, newest first.
func sessionFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	// Newest first: a search is almost always about the recent work.
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if info, err := os.Stat(files[j]); err == nil {
				if other, err := os.Stat(files[i]); err == nil && info.ModTime().After(other.ModTime()) {
					files[i], files[j] = files[j], files[i]
				}
			}
		}
	}
	return files, nil
}

// sessionLine is one entry of a pi session file.
type sessionLine struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// scanSessionFile reads one conversation and returns the matching messages.
func scanSessionFile(path, text string, caseSensitive bool, limit int) []Hit {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	if info, err := file.Stat(); err != nil || info.Size() > maxSessionBytes {
		return nil
	}
	needle := text
	foldedNeedle := ""
	if !caseSensitive {
		foldedNeedle = foldRunes(text)
	}
	length := utf8.RuneCountInString(text)
	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	hits := make([]Hit, 0, 2)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	for scanner.Scan() {
		var entry sessionLine
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		// The session id lives on the file's header entry, not on every message.
		if entry.SessionID != "" {
			sessionID = entry.SessionID
		}
		if entry.Type != "message" {
			continue
		}
		role := entry.Message.Role
		if role != "user" && role != "assistant" {
			continue
		}
		content := messageText(entry.Message.Content)
		index := matchIndex(content, needle, foldedNeedle)
		if index < 0 {
			continue
		}
		hits = append(hits, Hit{
			Kind:      "message",
			Path:      path,
			Text:      excerpt(content, index, length),
			SessionID: sessionID,
			Role:      role,
			At:        entry.Timestamp,
		})
		if len(hits) >= limit {
			break
		}
	}
	return hits
}

// matchIndex answers where a query starts in one message, as a rune index — the unit the
// excerpt window is cut in.
//
// Folding and cutting have to share a unit: unicode.ToLower maps one rune to one rune but
// not one byte to one byte (lower-casing "İ" yields a single byte where the original has
// two), so a byte offset taken from the folded text points somewhere else in the original
// and a window cut there shows text that has nothing to do with the match. foldedNeedle is
// empty for a case-sensitive query.
func matchIndex(content, needle, foldedNeedle string) int {
	if foldedNeedle == "" {
		byteIndex := strings.Index(content, needle)
		if byteIndex < 0 {
			return -1
		}
		return utf8.RuneCountInString(content[:byteIndex])
	}
	folded := foldRunes(content)
	byteIndex := strings.Index(folded, foldedNeedle)
	if byteIndex < 0 {
		return -1
	}
	return utf8.RuneCountInString(folded[:byteIndex])
}

// foldRunes lower-cases one rune at a time, so the result has exactly as many runes as its
// input — which is what keeps an index into the folded text usable on the original.
func foldRunes(text string) string {
	folded := make([]rune, 0, len(text))
	for _, char := range text {
		folded = append(folded, unicode.ToLower(char))
	}
	return string(folded)
}

// messageText flattens the content blocks of one message into its text.
func messageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var direct string
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var buffer strings.Builder
	for _, block := range blocks {
		if block.Type != "text" {
			continue
		}
		if buffer.Len() > 0 {
			buffer.WriteString("\n")
		}
		buffer.WriteString(block.Text)
	}
	return buffer.String()
}

// excerpt returns a window of content around the match, so a client does not have
// to render a whole message to show why it matched.
//
// The index and the length are rune counts and the window is cut on rune boundaries: a
// byte offset would split a character in two, and the response is JSON — the client would
// read a replacement character instead of the text it searched for.
func excerpt(content string, index, length int) string {
	const padding = 60
	runes := []rune(content)
	start := index - padding
	if start < 0 {
		start = 0
	}
	end := index + length + padding
	if end > len(runes) {
		end = len(runes)
	}
	if start > end {
		start = end
	}
	window := strings.TrimSpace(string(runes[start:end]))
	if start > 0 {
		window = "…" + window
	}
	if end < len(runes) {
		window += "…"
	}
	return window
}
