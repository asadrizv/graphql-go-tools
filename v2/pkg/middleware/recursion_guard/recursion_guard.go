// Package recursion_guard detects excessive recursion depth in GraphQL queries.
//
// The guard counts repetitions of the **same field on the same parent type**
// along a single ancestry chain.  Cycles that revisit the same return type via
// a different field are allowed.
package recursion_guard

import (
	"fmt"
	"strings"

	"github.com/wundergraph/graphql-go-tools/pkg/graphql"

	"github.com/wundergraph/graphql-go-tools/v2/pkg/ast"
	"github.com/wundergraph/graphql-go-tools/v2/pkg/astvisitor"
	"github.com/wundergraph/graphql-go-tools/v2/pkg/operationreport"
)

// RecursionGuard enforces a max repeat‑depth for each (parentType.field) pair.
type RecursionGuard struct{ MaxDepth int }

func NewRecursionGuard(maxDepth int) *RecursionGuard { return &RecursionGuard{maxDepth} }

// public helper (used by Cosmo router)
func ValidateRecursion(maxDepth int, op, schema *ast.Document, rep *operationreport.Report) graphql.Errors {
	if maxDepth <= 0 {
		return graphql.RequestErrors{{Message: "Recursion guard max depth must be greater than 0"}}
	}
	NewRecursionGuard(maxDepth).Do(op, schema, rep)
	return nil
}

func (g *RecursionGuard) Do(op, schema *ast.Document, rep *operationreport.Report) {
	v := &visitor{
		maxDepth:   g.MaxDepth,
		op:         op,
		schema:     schema,
		report:     rep,
		pairCount:  map[string]int{},
		path:       []string{},
		frameStack: []frame{},
	}

	w := astvisitor.NewWalker(48)
	w.RegisterEnterSelectionSetVisitor(v)
	w.RegisterLeaveSelectionSetVisitor(v)
	w.RegisterEnterFieldVisitor(v)
	v.Walker = &w
	w.Walk(op, schema, rep)
}

type frame struct {
	startPath int
	bumped    []string
}

type visitor struct {
	*astvisitor.Walker
	op, schema *ast.Document
	report     *operationreport.Report
	maxDepth   int

	pairCount  map[string]int
	path       []string
	frameStack []frame
	errHit     bool
}

// build key "ParentType.field"
func (v *visitor) pairKey(ref int) (string, bool) {
	// v.EnclosingTypeDefinition is an *ast.Node* (not a func).
	parentNode := v.EnclosingTypeDefinition

	// 0‑value means "none" (root selection / introspection).
	if parentNode.Kind == 0 {
		return "", false
	}

	var parentName string
	switch parentNode.Kind {
	case ast.NodeKindObjectTypeDefinition:
		parentName = v.schema.ObjectTypeDefinitionNameString(parentNode.Ref)
	case ast.NodeKindInterfaceTypeDefinition:
		parentName = v.schema.InterfaceTypeDefinitionNameString(parentNode.Ref)
	default:
		return "", false // scalars, unions, etc.
	}

	field := v.op.FieldNameString(ref)
	return parentName + "." + field, true
}

func named(doc *ast.Document, t int) int {
	for doc.Types[t].TypeKind != ast.TypeKindNamed {
		t = doc.Types[t].OfType
	}
	return t
}

func (v *visitor) EnterSelectionSet(ref int) {
	if len(v.Ancestors) == 0 || v.Ancestors[len(v.Ancestors)-1].Kind != ast.NodeKindField {
		return
	}
	v.frameStack = append(v.frameStack, frame{startPath: len(v.path)})
}

func (v *visitor) LeaveSelectionSet(ref int) {
	if len(v.frameStack) == 0 || len(v.Ancestors) == 0 ||
		v.Ancestors[len(v.Ancestors)-1].Kind != ast.NodeKindField {
		return
	}

	for pair, n := range v.pairCount {
		if n > v.maxDepth {
			v.report.AddExternalError(operationreport.ExternalError{
				Message: fmt.Sprintf(
					"Recursion detected: %q exceeds depth %d at path %q",
					pair, v.maxDepth, strings.Join(v.path, "."),
				),
			})
			v.errHit = true
			break
		}
	}

	fr := v.frameStack[len(v.frameStack)-1]
	for _, p := range fr.bumped {
		if v.pairCount[p]--; v.pairCount[p] == 0 {
			delete(v.pairCount, p)
		}
	}
	v.frameStack = v.frameStack[:len(v.frameStack)-1]
	if fr.startPath < len(v.path) {
		v.path = v.path[:fr.startPath]
	}
}

func (v *visitor) EnterField(ref int) {
	if v.errHit {
		return
	}

	v.path = append(v.path, v.op.FieldAliasOrNameString(ref))

	def, ok := v.FieldDefinition(ref)
	if !ok {
		return
	}
	tn := v.schema.TypeNameString(named(v.schema, v.schema.FieldDefinitionType(def)))
	node, exists := v.schema.Index.FirstNodeByNameStr(tn)
	if !exists || (node.Kind != ast.NodeKindObjectTypeDefinition && node.Kind != ast.NodeKindInterfaceTypeDefinition) {
		return
	}

	pair, ok := v.pairKey(ref)
	if !ok {
		return
	}

	v.pairCount[pair]++

	if len(v.frameStack) > 0 {
		top := &v.frameStack[len(v.frameStack)-1]
		top.bumped = append(top.bumped, pair)
	}
}
