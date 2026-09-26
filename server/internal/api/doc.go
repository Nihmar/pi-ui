// Package api is the REST surface of the spike (docs/spike-interfaces.md §5.4): a
// net/http ServeMux with method+wildcard patterns, JSON everywhere and one Authenticator
// seam that Phase 3 replaces without touching a handler.
//
// Everything is injected through Options, so the package has no global state and a test
// builds a router with a fake supervisor, a fake hub and a fixed Scope.
//
// Errors use the shared taxonomy of §11 and the HTTP mapping of PLAN.md §4.5:
//
//	400 bad_request      401 unauthorized   403 forbidden_scope
//	404 not_found        409 session_limit  409 busy_streaming   409 already_answered
//	422 pi_rejected      500 internal       502 pi_error        504 timeout
package api
