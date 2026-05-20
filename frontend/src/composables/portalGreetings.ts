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

// Customer-portal greeting. The voice differs from the internal
// `composables/greetings.ts`: portal users are customers, not the
// engineers building PAIMOS, so the messages are warmer, calmer,
// and avoid the "ship it / every commit counts" coachy register.
// Target register: same Apple-Notes-Stil German release-note copy
// the v3.5 customer_rewrite action produces, mirrored in EN.

export type PortalLocale = "de" | "en";

interface GreetingParts {
  prefix: string;
  name: string;
  message: string;
}

const MESSAGES: Record<PortalLocale, readonly string[]> = {
  en: [
    "Welcome back. Here is the current state of your projects.",
    "We are glad you are here.",
    "A calm look at where things stand today.",
    "Every project moves a little further each week.",
    "Take a moment to see what is ready for your review.",
    "Transparency at the pace of your work.",
    "A clear picture, without the noise.",
    "Your projects, at a glance.",
    "Progress is best when it is visible.",
    "Steady hands, steady results.",
    "Here for the long run with you.",
    "Quietly making things happen.",
    "Good work takes its time, and shows it.",
    "Thank you for your trust.",
    "A short visit can save a long meeting.",
    "The details are here when you need them.",
    "Everything in one place, as it should be.",
    "Care shows up in the small things.",
    "Your feedback shapes what we build next.",
    "We keep the moving parts moving.",
    "Done is something to be confirmed, not assumed.",
    "Calm progress is still progress.",
    "Built with care, reviewed with you.",
    "Every accepted ticket is a small handshake.",
    "A good day for a short status check.",
    "Less noise, more clarity.",
    "Your projects deserve careful attention.",
    "Slow is smooth, smooth is steady.",
    "We are right where you left us.",
    "A quiet space for thoughtful decisions.",
    "Real progress, no theatrics.",
    "Here when you need us, ready when you are.",
    "Honest work, plainly shown.",
    "Each step recorded, each decision yours.",
    "A workshop, not a stage.",
    "Trust, line by line.",
  ],
  de: [
    "Willkommen zurück. Hier ist der aktuelle Stand Ihrer Projekte.",
    "Schön, dass Sie da sind.",
    "Ein ruhiger Blick auf den heutigen Stand.",
    "Jedes Projekt bewegt sich jede Woche ein Stück weiter.",
    "Nehmen Sie sich einen Moment für die Punkte zur Freigabe.",
    "Transparenz im Takt Ihrer Arbeit.",
    "Klarheit, ohne den Lärm.",
    "Ihre Projekte auf einen Blick.",
    "Fortschritt ist am schönsten, wenn er sichtbar ist.",
    "Ruhige Hand, verlässliche Ergebnisse.",
    "Wir sind langfristig an Ihrer Seite.",
    "Leise weiterarbeiten, sauber abliefern.",
    "Gute Arbeit braucht ihre Zeit und zeigt sie auch.",
    "Vielen Dank für Ihr Vertrauen.",
    "Ein kurzer Besuch erspart ein langes Meeting.",
    "Die Details sind hier, wenn Sie sie brauchen.",
    "Alles an einem Ort, so wie es sein soll.",
    "Sorgfalt zeigt sich im Detail.",
    "Ihr Feedback prägt, was als nächstes entsteht.",
    "Wir halten die Räder am Laufen.",
    "Fertig wird bestätigt, nicht angenommen.",
    "Ruhiger Fortschritt ist trotzdem Fortschritt.",
    "Mit Sorgfalt gebaut, mit Ihnen abgenommen.",
    "Jede Annahme ist ein kleiner Handschlag.",
    "Ein guter Tag für einen kurzen Statuscheck.",
    "Weniger Lärm, mehr Klarheit.",
    "Ihre Projekte verdienen Aufmerksamkeit.",
    "Schritt für Schritt, gleichmäßig vorwärts.",
    "Wir sind genau dort, wo Sie uns verlassen haben.",
    "Ein ruhiger Raum für sorgsame Entscheidungen.",
    "Echter Fortschritt, ohne Theater.",
    "Hier, wenn Sie uns brauchen.",
    "Ehrliche Arbeit, klar dargestellt.",
    "Jeder Schritt nachvollziehbar, jede Entscheidung Ihre.",
    "Eine Werkstatt, keine Bühne.",
    "Vertrauen, Zeile für Zeile.",
  ],
};

const PREFIXES: Record<PortalLocale, { morning: string; afternoon: string; evening: string }> = {
  en: { morning: "Good morning", afternoon: "Good afternoon", evening: "Good evening" },
  de: { morning: "Guten Morgen", afternoon: "Guten Tag", evening: "Guten Abend" },
};

const FALLBACK_NAME: Record<PortalLocale, string> = {
  en: "there",
  de: "dort",
};

function prefixForHour(hour: number, locale: PortalLocale): string {
  const set = PREFIXES[locale];
  if (hour >= 5 && hour < 12) return set.morning;
  if (hour >= 12 && hour < 18) return set.afternoon;
  return set.evening;
}

function dayOfYear(d: Date): number {
  const start = new Date(d.getFullYear(), 0, 0);
  return Math.floor((d.getTime() - start.getTime()) / 86_400_000);
}

export function portalGreeting(
  firstName: string | undefined,
  username: string,
  locale: PortalLocale,
  now: Date = new Date(),
): GreetingParts {
  const safeLocale: PortalLocale = locale === "de" ? "de" : "en";
  const prefix = prefixForHour(now.getHours(), safeLocale);
  const name = (firstName?.trim() || username?.trim()) || FALLBACK_NAME[safeLocale];
  const list = MESSAGES[safeLocale];
  const message = list[dayOfYear(now) % list.length];
  return { prefix, name, message };
}

export const _portalGreetingsForTest = MESSAGES;
