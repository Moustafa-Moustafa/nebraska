package handler

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/flatcar/nebraska/backend/pkg/codegen"
	"github.com/flatcar/nebraska/backend/pkg/handler/admin"
	"github.com/flatcar/nebraska/backend/pkg/handler/runtime"
	custommiddleware "github.com/flatcar/nebraska/backend/pkg/middleware"
)

func TestMain(m *testing.M) {
	if os.Getenv("NEBRASKA_SKIP_TESTS") != "" {
		return
	}

	os.Exit(m.Run())
}

// nodeRoleExtension mirrors the extension the node role guard reads in
// pkg/middleware, so this test fails if the two ever name different things.
const nodeRoleExtension = "x-nebraska-node-role"

// TestEndpointsMatchSpecNodeRoles keeps the sub-package an endpoint is served
// from in step with the node role declared for it in api/spec.yaml.
func TestEndpointsMatchSpecNodeRoles(t *testing.T) {
	swagger, err := codegen.GetSwagger()
	require.NoError(t, err)

	adminMethods := methodNames(reflect.TypeOf((*admin.Handler)(nil)))
	runtimeMethods := methodNames(reflect.TypeOf((*runtime.Handler)(nil)))
	served := methodNames(reflect.TypeOf((*Handler)(nil)))

	for path, item := range swagger.Paths.Map() {
		for method, operation := range item.Operations() {
			where := method + " " + path

			role, ok := operation.Extensions[nodeRoleExtension].(string)
			require.Truef(t, ok, "%s: no %s", where, nodeRoleExtension)

			name := goMethodName(operation.OperationID)

			require.Truef(t, served[name], "%s: %s is not served by the handler", where, name)

			switch role {
			case custommiddleware.RoleAdmin:
				require.Truef(t, adminMethods[name], "%s: %s is %q but is not in pkg/handler/admin", where, name, role)
				require.Falsef(t, runtimeMethods[name], "%s: %s is %q but is in pkg/handler/runtime", where, name, role)
			case custommiddleware.RoleRuntime:
				require.Truef(t, runtimeMethods[name], "%s: %s is %q but is not in pkg/handler/runtime", where, name, role)
				require.Falsef(t, adminMethods[name], "%s: %s is %q but is in pkg/handler/admin", where, name, role)
			case custommiddleware.RoleNone:
				require.Falsef(t, adminMethods[name], "%s: %s touches no database but is in pkg/handler/admin", where, name)
				require.Falsef(t, runtimeMethods[name], "%s: %s touches no database but is in pkg/handler/runtime", where, name)
			default:
				t.Fatalf("%s: unknown %s %q", where, nodeRoleExtension, role)
			}
		}
	}
}

// goMethodName is how oapi-codegen names the ServerInterface method of an operation.
func goMethodName(operationID string) string {
	return strings.ToUpper(operationID[:1]) + operationID[1:]
}

func methodNames(t reflect.Type) map[string]bool {
	names := make(map[string]bool, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		names[t.Method(i).Name] = true
	}
	return names
}
