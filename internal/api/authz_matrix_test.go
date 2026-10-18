package api

import (
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/auth"
)

// matrixRequest is one concrete call of an operation.
type matrixRequest struct {
	method, path, body string
}

// matrixRequests holds a request for every operation in the spec. Requests
// that change state are ordered last, and DeleteProvider targets a spare
// provider, so earlier rows see the fixture unchanged.
var matrixRequests = []struct {
	operation string
	req       matrixRequest
}{
	{"GetHealthz", matrixRequest{"GET", "/healthz", ""}},
	{"GetReadyz", matrixRequest{"GET", "/readyz", ""}},
	{"GetVersion", matrixRequest{"GET", "/api/v1/version", ""}},
	// An empty body is refused before rate limiting and password hashing,
	// so the row is identical for every caller.
	{"Login", matrixRequest{"POST", "/api/v1/auth/login", ""}},
	{"GetMe", matrixRequest{"GET", "/api/v1/auth/me", ""}},
	{"ListSites", matrixRequest{"GET", "/api/v1/sites", ""}},
	{"ListProjects", matrixRequest{"GET", "/api/v1/projects", ""}},
	{"ListProjectMappings", matrixRequest{"GET", "/api/v1/projects/payment/mappings", ""}},
	{"ListProviders", matrixRequest{"GET", "/api/v1/providers", ""}},
	{"GetProvider", matrixRequest{"GET", "/api/v1/providers/pve-lab", ""}},
	{"ListResources", matrixRequest{"GET", "/api/v1/resources?kind=vm", ""}},
	{"GetCapacity", matrixRequest{"GET", "/api/v1/capacity", ""}},
	// Callers outside p1's scope get 404, which still counts as access to
	// the operation; visibility itself is checked in the inventory tests.
	{"GetResource", matrixRequest{"GET", "/api/v1/resources/pve-lab/vm/p1", ""}},
	{"CreateConsoleSession", matrixRequest{"POST", "/api/v1/resources/pve-lab/vm/p1/console", `{"kind":"vnc"}`}},
	{"GetProviderSyncStatus", matrixRequest{"GET", "/api/v1/providers/pve-lab/sync-status", ""}},
	{"ListCredentials", matrixRequest{"GET", "/api/v1/providers/pve-lab/credentials", ""}},
	{"CreateSite", matrixRequest{"POST", "/api/v1/sites", `{"id":"site-m","name":"Matrix"}`}},
	{"CreateProject", matrixRequest{"POST", "/api/v1/projects", `{"id":"proj-m","name":"Matrix"}`}},
	{"CreateProjectMapping", matrixRequest{"POST", "/api/v1/projects/search/mappings", `{"providerId":"os-jkt","externalScope":"proj-search"}`}},
	{"UpdateProjectMapping", matrixRequest{"PATCH", "/api/v1/projects/search/mappings/999999", `{"externalScope":"pool:other"}`}},
	{"DeleteProjectMapping", matrixRequest{"DELETE", "/api/v1/projects/search/mappings/999999", ""}},
	{"CreateProvider", matrixRequest{"POST", "/api/v1/providers", `{"id":"prov-m","type":"fake","name":"Matrix","endpoints":["https://fake.example.com"]}`}},
	{"PutCredential", matrixRequest{"PUT", "/api/v1/providers/pve-lab/credentials", `{"purpose":"discovery","scope":"provider","secret":{"token_id":"t","token_secret":"s"}}`}},
	{"DeleteCredential", matrixRequest{"DELETE", "/api/v1/providers/pve-lab/credentials?purpose=discovery&scope=provider", ""}},
	{"UpdateProvider", matrixRequest{"PATCH", "/api/v1/providers/spare", `{"name":"Spare renamed"}`}},
	{"ValidateProvider", matrixRequest{"POST", "/api/v1/providers/spare/validate", ""}},
	{"DeleteProvider", matrixRequest{"DELETE", "/api/v1/providers/spare", ""}},
	{"Logout", matrixRequest{"POST", "/api/v1/auth/logout", ""}},
}

// roleChecked lists authenticated operations that also check the caller's
// role on the target, with the expected status per caller.
var roleChecked = map[string]func(bindings []auth.RoleBinding, globalAdmin bool) int{
	// p1 is a VM of project payment on pve-lab: consoles need an Admin or
	// Operator binding covering it; viewers who can see it get 403, callers
	// who cannot see it 404.
	"CreateConsoleSession": func(bs []auth.RoleBinding, globalAdmin bool) int {
		if globalAdmin {
			return http.StatusCreated
		}
		sees, operates := false, false
		for _, b := range bs {
			covers := (b.ProjectID == "" || b.ProjectID == "payment") && (b.ProviderID == "" || b.ProviderID == "pve-lab")
			if covers {
				sees = true
				operates = operates || b.Role == auth.RoleAdmin || b.Role == auth.RoleOperator
			}
		}
		switch {
		case operates:
			return http.StatusCreated
		case sees:
			return http.StatusForbidden
		default:
			return http.StatusNotFound
		}
	},
}

func TestAuthorizationMatrixCoversEveryOperation(t *testing.T) {
	ops := reflect.TypeOf((*gen.StrictServerInterface)(nil)).Elem()
	var covered []string
	for _, r := range matrixRequests {
		covered = append(covered, r.operation)
	}
	for i := 0; i < ops.NumMethod(); i++ {
		if name := ops.Method(i).Name; !slices.Contains(covered, name) {
			t.Errorf("operation %s has no row in the authorization matrix", name)
		}
	}
}

// TestAuthorizationMatrix checks every operation against every kind of
// caller (roadmap §6 Phase 1 exit criterion): public operations behave the
// same for everyone, authenticated ones refuse anonymous callers, and admin
// ones are reserved for global admins. A project- or provider-scoped admin
// binding is not a global admin.
func TestAuthorizationMatrix(t *testing.T) {
	a := newInventoryAPI(t)
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"spare","type":"fake","name":"Spare","endpoints":["https://spare.example.com"]}`), http.StatusCreated, nil)
	adminCookie := a.cookie

	// The global admin runs last because it is the only caller whose
	// mutations succeed.
	callers := []struct {
		name     string
		bindings []auth.RoleBinding
		anon     bool
	}{
		{name: "anonymous", anon: true},
		{name: "nobindings"},
		{name: "globalviewer", bindings: []auth.RoleBinding{{Role: auth.RoleViewer}}},
		{name: "globaloperator", bindings: []auth.RoleBinding{{Role: auth.RoleOperator}}},
		{name: "projectviewer", bindings: []auth.RoleBinding{{Role: auth.RoleViewer, ProjectID: "payment"}}},
		{name: "projectadmin", bindings: []auth.RoleBinding{{Role: auth.RoleAdmin, ProjectID: "payment"}}},
		{name: "provideradmin", bindings: []auth.RoleBinding{{Role: auth.RoleAdmin, ProviderID: "pve-lab"}}},
		{name: "globaladmin"},
	}

	publicStatus := map[string]int{}
	for _, c := range callers {
		t.Run(c.name, func(t *testing.T) {
			a.t = t
			switch {
			case c.anon:
				a.cookie = nil
			case c.name == "globaladmin":
				a.cookie = adminCookie
			default:
				a.loginAs(t, c.name, c.bindings...)
			}
			isAdmin := c.name == "globaladmin"
			for _, row := range matrixRequests {
				rec := a.call(row.req.method, row.req.path, row.req.body)
				got := rec.Code
				switch required := operationAccess[row.operation]; {
				case required == accessPublic:
					if want, seen := publicStatus[row.operation]; seen && got != want {
						t.Errorf("%s: status %d, other callers got %d", row.operation, got, want)
					}
					publicStatus[row.operation] = got
				case c.anon:
					if got != http.StatusUnauthorized {
						t.Errorf("%s: status %d, want 401", row.operation, got)
					}
				case required == accessAdmin && !isAdmin:
					if got != http.StatusForbidden {
						t.Errorf("%s: status %d, want 403", row.operation, got)
					}
				case roleChecked[row.operation] != nil:
					want := roleChecked[row.operation](c.bindings, isAdmin)
					if got != want {
						t.Errorf("%s: status %d, want %d; body: %s", row.operation, got, want, rec.Body.String())
					}
				default:
					if got == http.StatusUnauthorized || got == http.StatusForbidden || got >= 500 {
						t.Errorf("%s: status %d, want access; body: %s", row.operation, got, rec.Body.String())
					}
				}
			}
		})
	}
}

// TestScopedCallersSeeOnlyTheirProjects is the data half of the matrix:
// what each caller can read through the scoped endpoints.
func TestScopedCallersSeeOnlyTheirProjects(t *testing.T) {
	a := newInventoryAPI(t)
	cases := []struct {
		user     string
		bindings []auth.RoleBinding
		// vms is the VM count of GET /capacity; physical reports whether
		// hypervisor capacity is included (global bindings only).
		vms      int64
		physical bool
		// syncStatus is the status of GET /providers/{id}/sync-status.
		syncStatus map[string]int
		lastError  bool
	}{
		{"op", []auth.RoleBinding{{Role: auth.RoleOperator}}, 4, true,
			map[string]int{"pve-lab": 200, "os-jkt": 200}, true},
		{"globalviewer", []auth.RoleBinding{{Role: auth.RoleViewer}}, 3, true,
			map[string]int{"pve-lab": 200, "os-jkt": 200}, false},
		{"payviewer", []auth.RoleBinding{{Role: auth.RoleViewer, ProjectID: "payment"}}, 2, false,
			map[string]int{"pve-lab": 200, "os-jkt": 200}, false},
		{"searchviewer", []auth.RoleBinding{{Role: auth.RoleViewer, ProjectID: "search"}}, 1, false,
			map[string]int{"pve-lab": 200, "os-jkt": 404}, false},
		{"pvelead", []auth.RoleBinding{{Role: auth.RoleOperator, ProviderID: "pve-lab"}}, 2, false,
			map[string]int{"pve-lab": 200, "os-jkt": 404}, false},
		{"nobody", nil, 0, false,
			map[string]int{"pve-lab": 404, "os-jkt": 404}, false},
	}
	if _, err := a.st.RecordSyncFailure(t.Context(), "pve-lab", "dial tcp 192.0.2.10:8006: connection refused"); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.user, func(t *testing.T) {
			a.t = t
			a.loginAs(t, c.user, c.bindings...)
			var capacity gen.Capacity
			a.expect(a.call("GET", "/api/v1/capacity", ""), http.StatusOK, &capacity)
			if capacity.Vms.Total != c.vms {
				t.Errorf("capacity VMs = %d, want %d", capacity.Vms.Total, c.vms)
			}
			if (capacity.Hypervisors != nil) != c.physical {
				t.Errorf("hypervisor capacity present = %v, want %v", capacity.Hypervisors != nil, c.physical)
			}
			// Narrowing to a project never widens visibility.
			var narrowed gen.Capacity
			a.expect(a.call("GET", "/api/v1/capacity?projectId=search", ""), http.StatusOK, &narrowed)
			if narrowed.Vms.Total > capacity.Vms.Total || narrowed.Hypervisors != nil {
				t.Errorf("project-filtered capacity = %+v", narrowed)
			}
			for provider, want := range c.syncStatus {
				rec := a.call("GET", "/api/v1/providers/"+provider+"/sync-status", "")
				if rec.Code != want {
					t.Errorf("sync-status %s = %d, want %d", provider, rec.Code, want)
					continue
				}
				if provider != "pve-lab" || want != http.StatusOK {
					continue
				}
				var st gen.SyncStatus
				a.expect(rec, http.StatusOK, &st)
				if (st.LastError != "") != c.lastError {
					t.Errorf("lastError = %q, visible want %v", st.LastError, c.lastError)
				}
			}
		})
	}
}
