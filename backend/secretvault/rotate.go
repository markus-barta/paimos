// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

// PAI-261 phase 3 — rotate every secretvault-encrypted ciphertext from
// the current PAIMOS_SECRET_KEY to a new operator-supplied key. Used
// by the `paimos secrets rotate` backend subcommand. The CLI flow is:
//
//   1. Service is running on `PAIMOS_SECRET_KEY=OLD`.
//   2. Operator runs `paimos secrets rotate --new-key <NEW_BASE64>`
//      against the same DB while the service is stopped or
//      read-tolerant. Every ciphertext gets decrypted under OLD and
//      re-encrypted under NEW in a single transaction — either all
//      rows transition or none do.
//   3. Operator updates PAIMOS_SECRET_KEY=NEW, restarts the service.
//
// If step 2 fails for any row, the transaction rolls back: ALL rows
// stay on OLD. The service keeps running on OLD. The operator can
// retry rotation, no recovery needed. This is the property the
// existing model lacked — today, swapping PAIMOS_SECRET_KEY corrupts
// every row.
//
// Concurrent writes during rotation: out of scope. The expected flow
// is "stop the service, rotate, restart" and the CLI prints that
// guidance. A future enhancement could add `SELECT ... FOR UPDATE`
// semantics, but SQLite doesn't really do row locking and the
// stop-restart workflow is simpler to reason about.

package secretvault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
)

// RotateReport summarises a rotation pass. CRMRows and AIRows are the
// per-domain affected counts; CRMRows excludes provider_configs rows
// that have no secret (config_secret_json IS NULL or empty), AIRows
// excludes the ai_settings singleton when api_key_encrypted is unset.
//
// DryRun=true means the caller asked for counts only and no writes
// happened — see RotateOptions.
type RotateReport struct {
	CRMRows int
	AIRows  int
	DryRun  bool
}

// RotateOptions parameterises a rotation call. NewKey MUST be exactly
// 32 bytes (AES-256). DryRun=true makes Rotate read-only; it still
// decrypts each row to confirm the OLD key works against the data,
// then reports counts without writing the new ciphertext.
type RotateOptions struct {
	NewKey []byte
	DryRun bool
}

// ErrPartialRotation wraps the row-level error so the CLI can give
// the operator a specific row to investigate. The transaction is
// rolled back before this returns, so the DB is unchanged from
// before the call.
var ErrPartialRotation = errors.New("rotation failed mid-transaction; no rows changed")

// Rotate performs the OLD→NEW transition described in the package
// doc. The OLD key is sourced via RootKey() (env > disk, same as
// every other secretvault read), so this function inherits the
// process's current key without any extra plumbing. The NEW key
// comes from opts.
//
// The implementation walks two known consumer tables:
//
//   - provider_configs (CRM domain): config_secret_json BLOB.
//   - ai_settings (AI domain): api_key_encrypted BLOB.
//
// Adding a new consumer means adding a switch arm here AND filing
// the migration that introduces the new encrypted column. There is
// no auto-discovery on purpose: the rotation tool failing to know
// about a new column is preferable to it silently skipping rows.
func Rotate(ctx context.Context, db *sql.DB, opts RotateOptions) (RotateReport, error) {
	if len(opts.NewKey) != rootKeyBytes {
		return RotateReport{}, fmt.Errorf("new key must be %d bytes, got %d", rootKeyBytes, len(opts.NewKey))
	}
	oldKey, err := RootKey()
	if err != nil {
		return RotateReport{}, fmt.Errorf("read current key: %w", err)
	}

	report := RotateReport{DryRun: opts.DryRun}

	// Begin a transaction even on dry-run — gives us a consistent
	// snapshot to read from while the service is up. tx.Rollback() on
	// dry-run is the no-op we want.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RotateReport{}, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			log.Printf("secretvault.Rotate: tx.Rollback: %v", err)
		}
	}()

	if err := rotateCRM(ctx, tx, oldKey, opts, &report); err != nil {
		return report, fmt.Errorf("%w: crm: %v", ErrPartialRotation, err)
	}
	if err := rotateAI(ctx, tx, oldKey, opts, &report); err != nil {
		return report, fmt.Errorf("%w: ai_settings: %v", ErrPartialRotation, err)
	}

	if opts.DryRun {
		return report, nil
	}
	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("commit: %w", err)
	}
	return report, nil
}

func rotateCRM(ctx context.Context, tx *sql.Tx, oldKey []byte, opts RotateOptions, report *RotateReport) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT provider_id, config_secret_json
		FROM provider_configs
		WHERE config_secret_json IS NOT NULL AND length(config_secret_json) > 0
	`)
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	type cipherRow struct {
		providerID string
		cipher     []byte
	}
	var batch []cipherRow
	for rows.Next() {
		var r cipherRow
		if err := rows.Scan(&r.providerID, &r.cipher); err != nil {
			rows.Close()
			return fmt.Errorf("scan: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, r := range batch {
		plain, err := DecryptWithKey(oldKey, "crm:provider_configs", r.cipher)
		if err != nil {
			return fmt.Errorf("decrypt provider %s: %w", r.providerID, err)
		}
		if opts.DryRun {
			// Successful decrypt confirms the OLD key works — that
			// is the load-bearing check for the operator: dry-run
			// proves rotation can proceed before they commit to it.
			report.CRMRows++
			continue
		}
		newCipher, err := EncryptWithKey(opts.NewKey, "crm:provider_configs", plain)
		if err != nil {
			return fmt.Errorf("encrypt provider %s: %w", r.providerID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE provider_configs SET config_secret_json = ? WHERE provider_id = ?`,
			newCipher, r.providerID,
		); err != nil {
			return fmt.Errorf("update provider %s: %w", r.providerID, err)
		}
		report.CRMRows++
	}
	return nil
}

func rotateAI(ctx context.Context, tx *sql.Tx, oldKey []byte, opts RotateOptions, report *RotateReport) error {
	var existing []byte
	err := tx.QueryRowContext(ctx,
		`SELECT api_key_encrypted FROM ai_settings WHERE id = 1`,
	).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) || len(existing) == 0 {
		// No encrypted AI key yet; nothing to rotate. Plaintext-only
		// rows (pre-PAI-261) are also skipped here — the lazy
		// migration completes them on the next admin save.
		return nil
	}
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	plain, err := DecryptWithKey(oldKey, "ai:openrouter", existing)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	if opts.DryRun {
		report.AIRows++
		return nil
	}
	newCipher, err := EncryptWithKey(opts.NewKey, "ai:openrouter", plain)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE ai_settings SET api_key_encrypted = ? WHERE id = 1`,
		newCipher,
	); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	report.AIRows++
	return nil
}
