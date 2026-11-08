---
title: "Skill rows in the command list TUI design"
design-status: draft
design-approved-by: ""
design-approved-on: ""
---

The behavior was decided by the user in the task that asked for this change. The design file was written after the code, and it still needs the approval of the user.

## Outcome

The command list (`/`) shows the skills of the session as `/skill:<name>` rows with their description, after the built-in commands. Typing `/sk` or `/skill:kk` narrows the rows. Enter on a skill row puts `/skill:<name> ` in the editor and sends nothing. The transcript shows what the user typed, not the skill text.

## Non-goals

No new key, dialog, color, or glyph. No skill search by description. No skill picker box.

## Placement

The rows use the menu rows above the editor in the prompt pane (layout step 6, at most 8 rows). The skill rows are appended to the built-in rows. The user band of the transcript and the queue rows show `/skill:<name> args`.

## Sketches

80x24, draft `/s`:

```text
  ❯ /sessions       Watch another live session
    /skill:demo     A demo skill
    /skill:kk:plan  Plan a feature
 ❯ /s
```

80x24, draft `/` with more rows than fit (the selection moved to the last row):

```text
    /take            Take control of this session
    /copy            Copy the last reply
    /quit            Detach and quit
    /skill:alpha     First skill
    /skill:beta      Second skill
  ❯ /skill:gamma     Third skill
    /skill:delta     Fourth skill
  … 21 more
 ❯ /
```

## States and transitions

| State | Trigger | Next state |
|---|---|---|
| Built-in rows only | session start | skill rows added when `Client.Skills` answers |
| Built-in rows only | `Client.Skills` fails (older leader) | same, no notice |
| Rows of session A | `/new` or attach | rows of session B replace them |
| List open | Enter on a skill row | draft `/skill:<name> `, list closed, nothing sent |
| List open | Enter on a built-in row | the command runs, as before |

## Keys and focus

Up, Down, Esc, Enter and Alt+Enter keep their meaning. The list scrolls with the selection when it has more than 8 rows.

## Limited space and resize

The list never shows more than the rows that the layout gives it. A longer list shows a window and a dim `… N more` row.

## Text and width

A skill name and description are made safe (control characters become `?`). The row is cut to the terminal width with `…`.

## Style intent

Same roles as the built-in rows: `selected` for the chosen row, `muted` for descriptions, `dim` for the count row.

## Acceptance criteria

- At 100x30, after the session reports skills and the user types `/s`, the menu shows `/sessions` first and the skill rows after it, each with its description.
- At 100x30, after `/skill:de` and Enter, the editor shows `/skill:demo ` and the client received no prompt.
- With a leader that answers `unsupported` to the skills call, `/` shows the built-in commands only and no error text.
- After `/new`, the rows of the first session are gone and the rows of the second session show.

## Open questions

None.
