package graphql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wundergraph/graphql-go-tools/v2/pkg/astnormalization"
	"github.com/wundergraph/graphql-go-tools/v2/pkg/astparser"
	"github.com/wundergraph/graphql-go-tools/v2/pkg/operationreport"
)

/* ── helper ─────────────────────────────────────────────────────────── */

func runRecursion(
	t *testing.T,
	sdl, query string,
	maxDepth int,
	wantErr bool,
) {
	schema, _ := astparser.ParseGraphqlDocumentString(sdl)
	op, _ := astparser.ParseGraphqlDocumentString(query)

	// flatten spreads / inline fragments once
	norm := operationreport.Report{}
	astnormalization.NormalizeOperation(&op, &schema, &norm)
	require.False(t, norm.HasErrors(), "bad test docs: %v", norm)

	res, err := NewRecursionCalculator(maxDepth).Calculate(&op, &schema)
	require.NoError(t, err)

	if wantErr {
		require.NotEmpty(t, res.Errors, "expected recursion error but got none")
	} else {
		require.Empty(t, res.Errors, "unexpected recursion error: %v", res.Errors)
	}
}

/* ── base scalars used by every SDL ─────────────────────────────────── */

const scalars = "scalar ID\nscalar String\n"

/* ── SDLs exactly mirroring the spec examples ───────────────────────── */

const employeeSDL = scalars + `
type Query   { employee(id: ID!): Employee }
type Employee{ id: ID manager: Employee }
schema { query: Query }`

const bookSDL = scalars + `
type Query  { book(id: ID!): Book }
type Book   { id: ID author: Author }
type Author { id: ID works: [Book] coauthor: CoAuthor }
type CoAuthor { id: ID works: [Book] }
schema { query: Query }`

const userSDL = scalars + `
type Query { user(id: ID!): User }
type User  { id: ID name: String posts: [Post] friends: [User] }
type Post  { id: ID title: String author: User }
schema { query: Query }`

/* ── queries from the assignment & your explanation ────────────────── */

// direct recursion Employee.manager.manager
const directRecursion = `
{
  employee(id:"123"){
    manager{
      manager{ id }
    }
  }
}`

// indirect loop Book → Author → Book
const indirectRecursion = `
{
  book(id:"1"){
    author{
      works{
        author{
          works{ id }
        }
      }
    }
  }
}`

// cycle only: User.posts → Post.author(User)  (no pair repeats)
const cyclicPath = `
{
  user(id:"1"){
    posts{
      author{
      id
      coauthor{ id }
     }
    }
  }
}`

// true recursion: User.friends.friends  …
const friendsRecursion = `
{
  user(id:"1"){
    friends{
		name
      friends{
        name
      }
    }
  }
}`

/* ── tests ──────────────────────────────────────────────────────────── */

func TestRecursionCalculator(t *testing.T) {
	/* employee examples ------------------------------------------------ */

	runRecursion(t, employeeSDL, `{ employee(id:"1"){ id } }`, 1, false) // scalar

	t.Run("Employee.manager over limit – err", func(t *testing.T) {
		runRecursion(t, employeeSDL, directRecursion, 1, true)
	})
	t.Run("Employee.manager within limit – ok", func(t *testing.T) {
		runRecursion(t, employeeSDL, directRecursion, 3, false)
	})

	/* book / author loop ---------------------------------------------- */

	t.Run("Book loop within limit – ok", func(t *testing.T) {
		runRecursion(t, bookSDL, indirectRecursion, 2, false) // pair repeats twice, limit 2
	})
	t.Run("Book loop over limit – err", func(t *testing.T) {
		runRecursion(t, bookSDL, indirectRecursion, 1, true)
	})

	/* cyclic vs. recursive user examples ------------------------------ */

	t.Run("cyclic User-Post path – ok", func(t *testing.T) {
		runRecursion(t, userSDL, cyclicPath, 1, false) // no pair repeats
	})

	t.Run("User.friends recursion over limit – err", func(t *testing.T) {
		runRecursion(t, userSDL, friendsRecursion, 1, true) // pair User.friends repeats
	})
	t.Run("User.friends recursion within limit – ok", func(t *testing.T) {
		runRecursion(t, userSDL, friendsRecursion, 2, false)
	})
}
