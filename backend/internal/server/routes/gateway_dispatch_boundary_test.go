package routes

import (
	"github.com/stretchr/testify/require"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestGatewayRoutesContainNoPlatformDispatch(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "gateway.go", nil, 0)
	require.NoError(t, err)
	ast.Inspect(source, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.SwitchStmt, *ast.TypeSwitchStmt:
			t.Error("gateway route registration must delegate action policy to service")
		}
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if qualifier, ok := selector.X.(*ast.Ident); ok && (qualifier.Name == "gjson" || qualifier.Name == "sjson") {
				t.Error("routes must delegate model parsing/rewriting")
			}
		}
		return true
	})
}
