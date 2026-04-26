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

export default {
  portal: {
    title: 'Kundenportal',
    logout: 'Abmelden',
    search: 'Issues suchen…',
    yourProjects: 'Ihre Projekte',
    loading: 'Laden…',
    noProjects: 'Noch keine Projekte zugewiesen.',
    issues: 'Issues',
    done: 'Erledigt',
    allProjects: '← Alle Projekte',
    newRequest: '+ Neue Anfrage',
    tabs: {
      all: 'Alle',
      open: 'In Arbeit',
      review: 'Zur Abnahme',
      accepted: 'Abgenommen',
    },
    summary: {
      total: 'Gesamt',
      estimate: 'Schätzung',
      arCost: 'AR Kosten',
      report: 'Abnahmebericht',
    },
    reject: 'Ablehnen',
    invoicedLabel: 'Verrechnet',
    filters: {
      allStatus: 'Alle Status',
      allTypes: 'Alle Typen',
    },
    table: {
      key: 'Key',
      title: 'Titel',
      type: 'Typ',
      status: 'Status',
      priority: 'Priorität',
      estimate: 'Schätzung',
      ar: 'AR',
      accepted: 'Abgenommen',
    },
    noIssues: 'Keine Issues gefunden.',
    accept: 'Abnehmen',
    acceptedLabel: 'Abgenommen',
    requestModal: {
      title: 'Neue Anfrage',
      titleField: 'Titel',
      description: 'Beschreibung',
      cancel: 'Abbrechen',
      submit: 'Anfrage senden',
      submitting: 'Wird gesendet…',
    },
    issueDetail: {
      back: '← Zurück zu Issues',
      description: 'Beschreibung',
      acceptanceCriteria: 'Abnahmekriterien',
      status: 'Status',
      priority: 'Priorität',
      type: 'Typ',
      created: 'Erstellt',
      updated: 'Aktualisiert',
    },
  },
  status: {
    new: 'Neu',
    backlog: 'Backlog',
    'in-progress': 'In Bearbeitung',
    qa: 'QA',
    done: 'Erledigt',
    delivered: 'Geliefert',
    accepted: 'Abgenommen',
    invoiced: 'Verrechnet',
    cancelled: 'Storniert',
  },
  ai: {
    phase: {
      pending: 'Eingeplant',
      working: 'Läuft',
      stalled: 'Verzögert',
      failed: 'Fehlgeschlagen',
      cancelled: 'Abgebrochen',
    },
    phaseScript: {
      optimize: { reading: 'Liest deinen Text', composing: 'Formuliert straffer', refining: 'Feilt am Ton' },
      optimize_customer: { reading: 'Liest deinen Text', composing: 'Formuliert straffer', refining: 'Feilt am Ton' },
      translate: { reading: 'Liest die Vorlage', translating: 'Übersetzt den Entwurf', polishing: 'Poliert die Formulierung' },
      tone_check: { reading: 'Liest die Formulierung', screening: 'Prüft auf Drucksprache', softening: 'Mildert den Ton' },
      suggest_enhancement: { reading: 'Liest das Issue', probing: 'Sucht nach Verbesserungen', grouping: 'Bündelt konkrete Ideen' },
      spec_out: { reading: 'Liest das Issue', structuring: 'Strukturiert Akzeptanzkriterien', tightening: 'Schärft die Checkliste' },
      find_parent: { reading: 'Liest das Issue', scanning: 'Durchsucht den Projektbaum', ranking: 'Bewertet Parent-Kandidaten' },
      generate_subtasks: { reading: 'Liest das Issue', sequencing: 'Ordnet die Arbeitsschritte', sizing: 'Schätzt die Teilaufgaben' },
      estimate_effort: { reading: 'Liest Scope und AC', comparing: 'Vergleicht ähnliche Issues', weighing: 'Wägt die Komplexität ab' },
      detect_duplicates: { reading: 'Liest das Issue', matching: 'Vergleicht ähnliche Issues', ranking: 'Bewertet Duplikate' },
      ui_generation: { reading: 'Liest die Anfrage', drafting: 'Entwirft die UI-Spec', formatting: 'Formatiert die Ausgabe' },
    },
    providerSlow: 'Provider braucht länger als üblich',
    dismiss: 'Verwerfen',
    apply: 'Anwenden',
    details: 'Details',
    workingTitle: '{action} läuft',
    resultTitle: '{action} bereit',
    failedPrefix: 'AI fehlgeschlagen',
    modelLabel: 'Modell',
    tokensLabel: 'Tokens',
    detailsHint: 'Im Ergebnis-Modal lässt sich die vollständige Antwort prüfen und anwenden.',
    setAsParent: 'Top-Vorschlag als Parent setzen ({issueKey})?',
    applyEstimate: 'Diese Schätzung für das Issue übernehmen?',
    applyToneCheck: 'Den aktuellen Text durch den neutralisierten Entwurf ersetzen?',
    showReasoning: 'Begründung zeigen',
    linkAsRelated: 'Top-Treffer mit diesem Issue verknüpfen ({issueKey})?',
    linkRelated: 'Als related verknüpfen',
    linkBlocks: 'Blockiert',
    linkDependsOn: 'Hängt ab von',
    moreRelations: 'Mehr Relationen',
    undoTitle: 'Änderung übernommen',
    undoReady: 'Diese AI-Änderung kann kurz rückgängig gemacht werden.',
    undo: 'Rückgängig',
  },
  undo: {
    conflict: {
      fallbackTitle: 'Undo-Konflikt lösen',
      titleUndo: 'Undo braucht deine Auswahl',
      titleRedo: 'Redo braucht deine Auswahl',
      heroUndo: 'Seit der ursprünglichen Änderung wurden Felder erneut verändert.',
      heroRedo: 'Seit diesem Undo hat sich der Zustand erneut verändert.',
      heroBody: 'Nichts wird still überschrieben. Die konservativen Optionen sind vorausgewählt.',
      fieldHeader: 'Feldkonflikte',
      cascadeHeader: 'Kaskaden-Blocker',
      current: 'Aktuell',
      target: 'Ziel',
      cancel: 'Abbrechen',
      applying: 'Wird angewendet…',
      applyWithSelections: 'Mit Auswahl anwenden',
    },
  },
}
