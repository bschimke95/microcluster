package cluster

import (
	"database/sql"
	"fmt"

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
func PrepareStmts(db *sql.DB, skipErrors bool) error {
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
