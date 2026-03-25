# Prompt Arguments Reference

This document lists all template arguments prepared by the model executor for prompt rendering.

Source implementation:
- `pipeline_request.go` (`buildPromptContext`, `preparePromptArguments`, `preparePromptArgumentsForResponse`)

## Top-level Variables

The prompt context merges namespaced maps plus root-level conversation args.

Available top-level keys:
- `system`
- `assistant`
- `conversation`
- `session`
- `message`
- `args`
- `<root args>` (flattened copy of `communication.GetArgs()`)

## Namespaced Variables

### `system.*`
- `system.current_date` (`YYYY-MM-DD`, UTC)
- `system.current_time` (`HH:MM:SS`, UTC)
- `system.current_datetime` (RFC3339, UTC)
- `system.day_of_week` (e.g. `Monday`)
- `system.date_rfc1123`
- `system.date_unix` (seconds)
- `system.date_unix_ms` (milliseconds)

### `assistant.*`
- `assistant.name`
- `assistant.id` (string)
- `assistant.language`
- `assistant.description`

### `conversation.*`
- `conversation.id` (string)
- `conversation.identifier`
- `conversation.source`
- `conversation.direction`
- `conversation.created_date` (RFC3339, when available)
- `conversation.updated_date` (RFC3339, when available)
- `conversation.duration` (best-effort string from now - created_date)

### `session.*`
- `session.mode` (when `Communication` exposes `GetMode()`)

### `message.*`
- `message.text`
- `message.language`

For direct request flow (`UserTextPacket`), these come from the current input packet.

For recursive tool-follow-up flow, `message.text` and `message.language` are rebuilt from history:
- text: latest user message content from model history
- language precedence:
  1. latest user language in `communication.GetHistories()` (`SaveMessagePacket` first, then `UserTextPacket`)
  2. `communication.GetMetadata()["client.language"]`
  3. empty string

### `args.*`
- Full map from `communication.GetArgs()` is available at `args.*`

## Root Args (Non-namespaced)

All entries from `communication.GetArgs()` are also merged directly at root level.

Example:
- if args has `{ "name": "Prashant" }`, both are valid:
  - `{{ args.name }}`
  - `{{ name }}`

## Notes

- Template rendering is argument-only; no additional cached state is kept in the model executor.
- Recursive sends (tool follow-up) regenerate prompt arguments each time before request send.
