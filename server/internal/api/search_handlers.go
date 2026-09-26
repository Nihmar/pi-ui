package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Nihmar/pi-ui/server/internal/search"
)

// The search surface (docs/api-v1.md, "Search"): viewer only, because reading what
// is already on the host is what a read-only device is for.

// searchAll answers GET /api/v1/search?q=&scope=files,messages&cwd=&limit=.
func (a *api) searchAll(w http.ResponseWriter, r *http.Request) {
	service, ok := a.searchOrError(w)
	if !ok {
		return
	}
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	hits, err := service.Search(r.Context(), search.Query{
		Text:          query.Get("q"),
		Scope:         splitScope(query.Get("scope")),
		Dir:           query.Get("cwd"),
		CaseSensitive: strings.EqualFold(query.Get("case"), "sensitive"),
		Limit:         limit,
	})
	if err != nil {
		code := codeOr(err, "bad_request")
		writeError(w, statusFor(code), code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Hits []search.Hit `json:"hits"`
	}{Hits: hits})
}

// splitScope turns "files,messages" into the scope list the query expects; an empty
// value means both halves.
func splitScope(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := make([]string, 0, 2)
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

// searchOrError reports a server without a workspace instead of panicking.
func (a *api) searchOrError(w http.ResponseWriter) (*search.Service, bool) {
	if a.search == nil {
		writeError(w, http.StatusNotImplemented, "unsupported",
			"no workspace is configured on this server")
		return nil, false
	}
	return a.search, true
}
