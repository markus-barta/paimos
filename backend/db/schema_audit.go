package db

import "database/sql"

func CurrentSchemaVersion(db *sql.DB) (int, error) {
	var maxVersion int
	err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_versions`).Scan(&maxVersion)
	return maxVersion, err
}

func SchemaHasTable(db *sql.DB, name string) (bool, error) {
	var found string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil && found == name, err
}

func SchemaHasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func SchemaHasIndex(db *sql.DB, name string) (bool, error) {
	var found string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil && found == name, err
}

func ForeignKeysEnabled(db *sql.DB) (bool, error) {
	var enabled int
	err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled)
	return enabled == 1, err
}

func IntegrityCheckOK(db *sql.DB) (bool, error) {
	var status string
	err := db.QueryRow(`PRAGMA integrity_check`).Scan(&status)
	return status == "ok", err
}
