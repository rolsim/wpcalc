# wpcalc — user guide

Recording working hours in the monthly grid.

*Deutsch: [../de-CH/user.md](../de-CH/user.md)*

---

## Signing in

Open the address your administrator gave you and enter your username and
password. If you do not have an account, they must create one — there is no
self-registration.

Under WordPress there is no separate login: open **Working hours** in the
admin menu and your WordPress session is used.

## Reading the grid

Each row is one day of the month, each column one employee.

| | |
|---|---|
| **Shaded rows** | Saturday and Sunday |
| **Highlighted row** | today |
| **Grey cell with `·`** | outside that person's employment period — not editable |
| **Empty cell** | nothing recorded for that day |

Only employees whose employment overlaps the displayed month appear. Someone
who left in a previous month is not shown at all, rather than as a column of
locked cells.

## Entering hours

Click a cell and type the hours as a decimal number — **industrial minutes**,
where a quarter hour is `0.25`:

| You worked | You type |
|---|---|
| 7 hours 45 minutes | `7.75` or `7,75` |
| 8 hours | `8` or `8.00` |
| 30 minutes | `0.5` or `0,5` |
| 7 hours 3 minutes | `7.05` |

Both the comma and the dot are accepted, so it does not matter whether you use
the number pad or the keyboard.

**What is refused, rather than guessed at:**

- `7:45` — this field takes decimal hours, not hours and minutes
- `7.755` — more than two decimal places
- anything above `24.00` in one day, or a negative number
- text such as `7h30`

A rejected entry is reported and nothing is stored. Nothing is ever silently
rounded or reinterpreted: a wrong number in a timesheet is worse than a
visible error.

**To clear a cell,** delete its contents and save. The entry is removed, which
is not the same as recording zero.

## Saving

Your entry is saved when you leave the cell, or when you press **Enter**. A
brief green flash confirms it; a red outline means it was refused.

With JavaScript disabled every cell has its own **Save** button, and the page
reloads on each save. Everything works the same way, just more slowly.

### Keyboard

| Key | Does |
|---|---|
| **Enter** | save and move down one day |
| **↓** / **↑** | move down or up the same column, skipping locked cells |
| **Esc** | discard what you typed and restore the saved value |

Moving down a column is the fastest way to fill in a month for one person.

## Comments

The **Bemerkung / Comment** column at the right takes one note per day. It
belongs to the day, not to any one person — use it for things like a company
outing or a public holiday.

## Totals

Three totals, all calculated by the server:

- **bottom row** — total per employee for the month
- **right column** — total per day across everyone
- **corner** — the month's grand total

They update as you type. The same figures appear in the PDF reports, so the
screen and the printout cannot disagree.

## Moving between months

Use **← Previous month** and **Next month →**, or **Current month** to jump
back to today. There is no limit in either direction.

The address bar shows the month (`…/m/2026-07`), so you can bookmark one or
send a colleague a link to exactly the month you are looking at.

## Reports

**Auswertungen / Reports** produces PDFs:

- the monthly overview — everyone's totals for one month
- one employee, one month — day by day, including the day comments
- one employee, one year — month by month

## Language

The interface follows your browser's language setting. German (Swiss) and
English are available; anything else falls back to German. There is no
in-page language switcher — change your browser's preferred language to
switch.

## If something looks wrong

- **A cell will not accept anything.** It is grey with a `·`: the date is
  outside that person's employment period. Your administrator can adjust the
  start or end date.
- **Someone is missing from the grid.** Their employment does not overlap this
  month.
- **Your total looks wrong.** The totals only count the month on screen.
  Entries in a neighbouring month are not included.
