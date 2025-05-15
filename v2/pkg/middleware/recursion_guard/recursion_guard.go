// Package recursion_guard detects excessive recursion depth in GraphQL queries.
package recursion_guard

import (
	"fmt"
	"strings"

	"github.com/wundergraph/graphql-go-tools/pkg/graphql"

	"github.com/wundergraph/graphql-go-tools/v2/pkg/ast"
	"github.com/wundergraph/graphql-go-tools/v2/pkg/astvisitor"
	"github.com/wundergraph/graphql-go-tools/v2/pkg/operationreport"
)

type RecursionGuard struct{ MaxDepth int }

func NewRecursionGuard(maxDepth int) *RecursionGuard { return &RecursionGuard{maxDepth} }

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
	bumped    []string // (type.field) pairs bumped inside this set
}

type visitor struct {
	*astvisitor.Walker
	op, schema *ast.Document
	report     *operationreport.Report
	maxDepth   int

	pairCount  map[string]int // key = "Type.field"
	path       []string
	frameStack []frame
	errHit     bool
}

func named(doc *ast.Document, t int) int {
	for doc.Types[t].TypeKind != ast.TypeKindNamed {
		t = doc.Types[t].OfType
	}
	return t
}

func (v *visitor) newPair(ref int, typeName string) string {
	fieldName := v.op.FieldNameString(ref)
	return typeName + "." + fieldName // e.g. "User.friends"
}

func (v *visitor) EnterSelectionSet(ref int) {
	if len(v.Ancestors) == 0 || v.Ancestors[len(v.Ancestors)-1].Kind != ast.NodeKindField {
		return
	}
	v.frameStack = append(v.frameStack, frame{startPath: len(v.path)})
}

func (v *visitor) LeaveSelectionSet(ref int) {
	if len(v.frameStack) == 0 ||
		len(v.Ancestors) == 0 ||
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
	v.path = v.path[:fr.startPath]
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

	kindNode, exists := v.schema.Index.FirstNodeByNameStr(tn)
	if !exists ||
		(kindNode.Kind != ast.NodeKindObjectTypeDefinition &&
			kindNode.Kind != ast.NodeKindInterfaceTypeDefinition) {
		return // scalar / enum / union
	}

	pair := v.newPair(ref, tn)
	v.pairCount[pair]++

	if len(v.frameStack) > 0 {
		top := &v.frameStack[len(v.frameStack)-1]
		top.bumped = append(top.bumped, pair)
	}
}
