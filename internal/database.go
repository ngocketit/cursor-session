package internal

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

// OpenDatabase opens a SQLite database in read-only mode
func OpenDatabase(path string) (*sql.DB, error) {
	// Check if file exists when opening in read-only mode
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("database file does not exist: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	return db, nil
}

// QueryCursorDiskKV queries the cursorDiskKV table with a LIKE pattern.
// When the pattern is a simple prefix search (e.g. "bubbleId:%" or "bubbleId:chat1:%"),
// it uses a B-tree index range query (key >= prefix AND key < upperBound) instead of LIKE,
// avoiding a multi-gigabyte full table scan in SQLite.
func QueryCursorDiskKV(db *sql.DB, pattern string) ([]KeyValuePair, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if prefix, upperBound, ok := prefixRangeForPattern(pattern); ok {
		query := "SELECT key, value FROM cursorDiskKV WHERE key >= ? AND key < ? AND value IS NOT NULL"
		rows, err = db.Query(query, prefix, upperBound)
	} else {
		query := "SELECT key, value FROM cursorDiskKV WHERE key LIKE ? AND value IS NOT NULL"
		rows, err = db.Query(query, pattern)
	}
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pairs []KeyValuePair
	for rows.Next() {
		var pair KeyValuePair
		var value sql.NullString
		if err := rows.Scan(&pair.Key, &value); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		if value.Valid {
			pair.Value = value.String
			pairs = append(pairs, pair)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return pairs, nil
}

// prefixRangeForPattern converts a simple trailing-% pattern (without other SQL LIKE wildcards)
// into an inclusive lower bound and exclusive upper bound for B-tree index range scans.
func prefixRangeForPattern(pattern string) (string, string, bool) {
	if !strings.HasSuffix(pattern, "%") || len(pattern) < 2 {
		return "", "", false
	}
	prefix := pattern[:len(pattern)-1]
	// Fall back to LIKE if the prefix contains wildcard characters
	if strings.ContainsAny(prefix, "%_") {
		return "", "", false
	}
	lastByte := prefix[len(prefix)-1]
	if lastByte == 0xFF {
		return "", "", false
	}
	upperBound := prefix[:len(prefix)-1] + string(lastByte+1)
	return prefix, upperBound, true
}

// KeyValuePair represents a key-value pair from cursorDiskKV
type KeyValuePair struct {
	Key   string
	Value string
}
