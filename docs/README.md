# Muti Metroo Documentation

The Docusaurus site published at https://mutimetroo.com. Pages live in
`docs/`, the sidebar in `sidebars.ts`, and the site configuration in
`docusaurus.config.ts`.

## Writing

Read [STYLE.md](STYLE.md) before editing a page. The site follows the MDN
editorial model: every claim is verified against the source, reference
material comes before narrative, one canonical page per concept, and every
page ends with a See Also section.

Screenshots are generated, never hand-made. See
[scripts/README.md](scripts/README.md) for the capture rig.

## Local development

```bash
npm install
npm start          # dev server at http://localhost:3000 with live reload
npm run build      # production build into build/
npm run serve      # serve the production build
```

`npm run build` must finish without broken link or anchor warnings.

## Structure

| Directory          | Contents                                                  |
| ------------------ | --------------------------------------------------------- |
| `getting-started/` | Installation, quick start, setup wizard, first mesh        |
| `concepts/`        | Architecture, agent roles, transports, routing, streams    |
| `features/`        | Task-oriented usage guides                                 |
| `configuration/`   | One reference page per configuration section               |
| `deployment/`      | Service managers, containers, DLL mode, proxies, HA        |
| `security/`        | Encryption, TLS, authentication, access control            |
| `cli/`             | One reference page per top-level command                   |
| `api/`             | HTTP API reference, one page per endpoint group            |
| `troubleshooting/` | Common issues, connectivity, performance, FAQ              |

Pages for the companion tools (`mutiauk.mdx`, `muti-metroo-manager.mdx`) and
the download page sit at the top level.

When a page is renamed, merged, or removed, add a redirect in
`docusaurus.config.ts` (`@docusaurus/plugin-client-redirects`).

## Deployment

Every push to `master` that touches `docs/` builds the site and deploys it
to GitHub Pages (`.github/workflows/docs.yml`). The custom domain comes from
`static/CNAME`. Search is provided by `@easyops-cn/docusaurus-search-local`
and needs no external service.
