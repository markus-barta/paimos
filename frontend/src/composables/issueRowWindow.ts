/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

/**
 * IssueRowWindow — PAI-564 (IssueList v2 lazy-load / page-cache engine).
 *
 * A normalized window over one query's result set: rows stored by id, with a
 * separate ordered id list, plus the server's total and has-more so the UI can
 * say *precisely* whether it is showing all matching issues or only a loaded
 * subset. The window is keyed by the query fingerprint; a fingerprint change
 * resets it (incompatible window), while load-more appends without duplicating
 * rows or disturbing sort order. patch/remove support in-place row updates and
 * delta refresh (PAI-567 / PAI-568) without rebuilding the window.
 *
 * Pure / framework-free so it is exhaustively unit-testable; useIssueQuery
 * wraps it with reactivity.
 */
export class IssueRowWindow<T extends { id: number }> {
  private byId = new Map<number, T>()
  private order: number[] = []
  private _total = 0
  private _hasMore = false
  private _fingerprint = ''

  get fingerprint(): string { return this._fingerprint }
  get total(): number { return this._total }
  get loaded(): number { return this.order.length }
  get hasMore(): boolean { return this._hasMore }
  /** True when every matching row is loaded (showing all, not a subset). */
  get complete(): boolean { return !this._hasMore }

  has(id: number): boolean { return this.byId.has(id) }
  get(id: number): T | undefined { return this.byId.get(id) }

  /** Materialize the ordered rows (defensively skips any dropped ids). */
  rows(): T[] {
    const out: T[] = []
    for (const id of this.order) {
      const row = this.byId.get(id)
      if (row !== undefined) out.push(row)
    }
    return out
  }

  /** Drop everything and rebind to a new query fingerprint. */
  reset(fingerprint: string): void {
    this.byId.clear()
    this.order = []
    this._total = 0
    this._hasMore = false
    this._fingerprint = fingerprint
  }

  /** Replace the window (an offset-0 load) for `fingerprint`. */
  setWindow(rows: T[], total: number, hasMore: boolean, fingerprint: string): void {
    this.reset(fingerprint)
    this.add(rows)
    this._total = total
    this._hasMore = hasMore
  }

  /** Append a page; duplicate ids update in place and keep their position. */
  appendWindow(rows: T[], total: number, hasMore: boolean): void {
    this.add(rows)
    this._total = total
    this._hasMore = hasMore
  }

  private add(rows: T[]): void {
    for (const row of rows) {
      if (!this.byId.has(row.id)) this.order.push(row.id)
      this.byId.set(row.id, row) // latest snapshot wins; order preserved
    }
  }

  /** Replace a loaded row in place. Returns false if the id isn't loaded. */
  patch(row: T): boolean {
    if (!this.byId.has(row.id)) return false
    this.byId.set(row.id, row)
    return true
  }

  /**
   * Remove a row from the window (deleted, or moved out of the query).
   * Optionally decrement the total. Returns false if the id wasn't loaded.
   */
  remove(id: number, opts: { decrementTotal?: boolean } = {}): boolean {
    if (!this.byId.has(id)) return false
    this.byId.delete(id)
    const i = this.order.indexOf(id)
    if (i >= 0) this.order.splice(i, 1)
    if (opts.decrementTotal && this._total > 0) this._total -= 1
    return true
  }
}
