# Documentation Style Guide

This guide governs every page under `docs/docs/`. It follows the editorial
model of MDN Web Docs (https://developer.mozilla.org/en-US/docs/MDN/Writing_guidelines).
When a judgment call is not settled here, decide it the way an MDN editor would.

## Editorial model

- **Document what ships, not what is intended.** Every claim traces to the
  source code (`cmd/`, `internal/`), `configs/example.yaml`, or observed
  behavior of a built binary. If it cannot be verified, it does not go in.
- **Reference before narrative.** A reader looking something up gets an exact
  answer: the flag, key, or field, its type, its default, and what it does.
  Guides and examples are welcome but never displace reference material.
- **Neutral, plain, present tense.** No marketing voice. Avoid "simply",
  "easily", "just", "powerful", "seamless", "blazing", "effortless". State
  behavior and let the reader judge it.
- **One canonical page per concept.** Link to it instead of restating it.
  Reference detail for a config section lives on its configuration page; for
  a command on its CLI page; for an endpoint on its HTTP API page. Guides link
  to those pages rather than copying their tables.
- **Do not document bugs as features.** If the implementation looks wrong,
  report it instead of writing it into the docs.
- **Fix what you touch.** Read the whole page when editing it, not only the
  paragraph you came for.
- **Write for a reader who arrived from a search engine.** Each page stands on
  its own and does not assume the previous page was read.
- **Operators, not contributors.** No implementation internals: frame formats,
  wire encodings, stream state machines, package layout. Those belong in
  `Architecture.md` at the repository root.

## Mechanics

- **ASCII only.** No emojis, no curly quotes, no em or en dashes.
- **No ` -- ` as a separator in prose.** Use a comma, a colon, parentheses, or
  two sentences. Double dashes are only valid inside code, flags (`--yes`), and
  YAML.
- **Frontmatter** on every page: `title`, `description` (one sentence, used for
  SEO and link previews), and `sidebar_position` where the page lives in a
  sidebar category.
- **One H1** matching the title. Section headings use sentence case for
  descriptive headings; command, flag, key, and endpoint headings use the
  literal name in code format (for example `## route add`).
- **Every page ends with `## See Also`**: three to six links, each saying what
  the reader would go there for. This replaces "Related", "Next Steps", and
  "What's Next" sections.
- **No counts that drift** ("all 23 commands"), and no version numbers in prose
  unless they describe when a behavior changed.

## Examples

- **Examples run as written.** No `...` in place of values, no comments inside
  JSON, no pseudo-commands. Output blocks show real output from a built
  binary where practical, fenced as `text`.
- **Placeholders**, used consistently across the site:
  - Local HTTP API: `http://localhost:8080`
  - Local SOCKS listener: `localhost:1080` (`127.0.0.1:1080` in configs)
  - Public addresses: documentation ranges `192.0.2.0/24`, `198.51.100.0/24`,
    `203.0.113.0/24`, and hostnames under `example.com`
  - Private networks: RFC 1918 ranges (`10.0.0.0/8`, `192.168.0.0/16`)
  - Agent IDs: a full 32-character hex ID where a full ID is required, and the
    short 8-character form where output shows the short form
- **Code fences** carry a language: `bash` for commands, `yaml` for config,
  `json` for API bodies, `text` for command output.

## Reference pages

- **CLI pages** (`docs/cli/`): one page per top-level command. For each
  subcommand: synopsis, description, a flags table (flag, default,
  description), examples, and exit behavior where it differs from the
  defaults listed in `cli/overview`.
- **Configuration pages** (`docs/configuration/`): one page per top-level
  config section. A complete annotated YAML block, then a table of keys with
  type, default, and description. Defaults come from `internal/config`.
- **HTTP API pages** (`docs/api/`): one page per endpoint group. For each
  endpoint: method and path, authentication requirement, request body, response
  body with a real example, and error status codes.

## Admonitions

Use Docusaurus admonitions sparingly: `:::note`, `:::tip`, `:::warning`,
`:::danger`. A page where every second paragraph is an admonition has no
emphasis left.

## Images and screenshots

- **Screenshots come only from `docs/scripts/capture-screenshots.sh`.** It boots
  a throwaway mesh from the local source tree, stages the state each shot
  needs, captures, and tears everything down, so any run regenerates the same
  set. Never commit a hand-made screenshot; add a step to the script instead.
- Screenshots live in `docs/static/img/screenshots/<area>/NN-name.png` and are
  referenced with descriptive alt text that says what the image shows.
- Show terminal output as a `text` code block, not as an image.
- The mole mascot images in `static/img/` are decorative page headers. Keep the
  existing ones; do not add new ones to reference pages.

## Moving and removing pages

When a page is renamed, merged, or removed, add a redirect from the old URL in
`docusaurus.config.ts` (`@docusaurus/plugin-client-redirects`), and update the
sidebar in `sidebars.ts`.
