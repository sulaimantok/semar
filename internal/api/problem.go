package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sulaimantok/semar/internal/api/gen"
)

// newProblem builds an RFC 9457 problem (roadmap §3.10) using the type
// generated from api/openapi.yaml.
func newProblem(ctx context.Context, instance string, status int, detail string) gen.Problem {
	p := gen.Problem{
		Type:     "about:blank",
		Title:    http.StatusText(status),
		Status:   status,
		Instance: &instance,
	}
	if detail != "" {
		p.Detail = &detail
	}
	if id := RequestIDFrom(ctx); id != "" {
		p.RequestId = &id
	}
	return p
}

// writeProblemBody writes an already built problem.
func writeProblemBody(w http.ResponseWriter, status int, p gen.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// writeProblem writes a problem response outside the generated handlers
// (middleware, unknown routes, request/response errors).
func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(newProblem(r.Context(), r.URL.Path, status, detail))
}
