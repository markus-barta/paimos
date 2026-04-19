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

// Day-of-year keyed motivating messages — same message all day, changes each day.
const MESSAGES = [
  'Small steps compound into big wins.',
  'Today is a good day to ship something.',
  'Focus on what moves the needle.',
  'Progress, not perfection.',
  'Every commit counts.',
  'You have got this.',
  'What matters most today?',
  'Build something you are proud of.',
  'Less noise, more signal.',
  'Keep the momentum going.',
  'Great work starts with showing up.',
  'Think big, start small.',
  'One thing at a time.',
  'Ship early, iterate often.',
  'Your future self will thank you.',
  'Make it work, make it right.',
  'Clarity beats cleverness.',
  'Good enough today beats perfect never.',
  'Stay curious, stay building.',
  'You are closer than you think.',
  'Simplify, then ship.',
  'The best code is the code you don\'t write.',
  'Keep your focus tight.',
  'Solve the right problem first.',
  'Leave the codebase better than you found it.',
  'What can you unblock today?',
  'Done is better than discussed.',
  'Trust the process.',
  'One less TODO in the world.',
  'Consistency is a superpower.',
  'Own the outcome, not just the task.',
  'How can you make someone\'s day easier?',
  'Start with the hard part.',
  'Every expert was once a beginner.',
  'Keep it simple, keep it shipping.',
  'Your work matters more than you think.',
  'Take a breath. Then build.',
  'Momentum creates motivation.',
  'Plan less, learn more.',
  'What would make today a win?',
]

export function greeting(firstName: string | undefined, username: string): { prefix: string; name: string; message: string } {
  const h = new Date().getHours()
  const prefix = h >= 5 && h < 12 ? 'Good morning' : h >= 12 && h < 18 ? 'Good afternoon' : 'Good evening'
  const name = firstName || username || 'there'

  const now = new Date()
  const start = new Date(now.getFullYear(), 0, 0)
  const dayOfYear = Math.floor((now.getTime() - start.getTime()) / 86400000)
  const message = MESSAGES[dayOfYear % MESSAGES.length]

  return { prefix, name, message }
}
