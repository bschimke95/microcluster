package cluster

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/canonical/lxd/shared/logger"
)

var stmts = map[int]string{}            // Statement code to statement SQL text
var preparedStmts = map[int]*sql.Stmt{} // Statement code to SQL statement.

// RegisterStmt register a SQL statement.
//
// Registered statements will be prepared upfront and re-used, to speed up
// execution.
//
// Return a unique registration code.
func RegisterStmt(sql string) int {
	if stmts == nil {
		stmts = map[int]string{}
	}

	// Have a unique code for each statement.
	code := len(stmts) + 1

	stmts[code] = sql
	return code
}

// PrepareStmts prepares all registered statements and stores them in preparedStmts.
// The project argument is kept for backwards compatibility but is deprecated.
func PrepareStmts(db *sql.DB, project string, skipErrors bool) error {
	logger.Infof("Preparing statements")

	for code, stmt := range stmts {
		preparedStmt, err := db.Prepare(stmt)
		if err != nil && !skipErrors {
			return fmt.Errorf("%q: %w", stmt, err)
		}

		preparedStmts[code] = preparedStmt
	}

	return nil
}

// Stmt prepares the in-memory prepared statement for the transaction.
func Stmt(tx *sql.Tx, code int) (*sql.Stmt, error) {
	stmt, ok := preparedStmts[code]
	if !ok {
		return nil, fmt.Errorf("No prepared statement registered with code %d", code)
	}

	return tx.Stmt(stmt), nil
}

// StmtString returns the in-memory query string with the given code.
func StmtString(code int) (string, error) {
	for stmtCode, stmt := range stmts {
		if stmtCode == code {
			return stmt, nil
		}
	}

	return "", fmt.Errorf("No prepared statement registered with code %d", code)
}

// GetCallerProject will get the go project name of whichever function called `GetCallerProject`.
//
// Deprecated: The caller project is no longer required and causes issues when vendoring.
func GetCallerProject() string {
	sep := string(os.PathSeparator)

	// Get the caller of whoever called this function.
	_, file, _, _ := runtime.Caller(2)

	// The project may be a snap build path of the form ...parts/<project>/build....
	_, after, ok := strings.Cut(file, fmt.Sprintf("parts%s", sep))
	if ok {
		project, _, ok := strings.Cut(after, fmt.Sprintf("%sbuild", sep))
		if ok {
			return project
		}
	}

	// If not a snap build path, the project may be in a go module path of the form .../project@version....
	before, _, ok := strings.Cut(file, "@")
	base := filepath.Base(before)
	if ok && base != "" {
		// If the base path is a go module version like v2, the project name will be one level down.
		exp := regexp.MustCompile(`^v\d+$`)
		if exp.MatchString(base) {
			return filepath.Base(filepath.Dir(before))
		}

		return base
	}

	// If not a go module path,	assume a GOPATH of the form example.com/author/project/packages....
	_, after, _ = strings.Cut(file, fmt.Sprintf("%ssrc%s", sep, sep))
	tree := strings.Split(after, sep)
	if len(tree) >= 3 {
		return tree[2]
	}

	return ""
}
