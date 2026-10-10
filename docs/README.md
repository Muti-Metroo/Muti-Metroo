# Muti Metroo Documentation

This directory contains the Docusaurus documentation site for Muti Metroo.

## Getting Started

### Install Dependencies

```bash
npm install
```

### Local Development

```bash
npm start
```

This starts a local development server at `http://localhost:3000` with live reload.

### Build

```bash
npm run build
```

Builds the static site to the `build` directory.

### Serve Built Site

```bash
npm run serve
```

Serves the built site locally for testing.

## Documentation Structure

```
docs/
├── intro.md                    # Introduction and overview
├── getting-started.md          # Getting started guide
├── installation.md             # Installation instructions
├── quick-start.md              # Quick start manual setup
├── interactive-setup.md        # Interactive wizard guide
├── configuration.md            # Configuration reference
├── architecture/               # Architecture documentation
│   ├── overview.md
│   ├── agent-roles.md
│   ├── data-flow.md
│   └── packages.md
├── features/                   # Feature documentation
│   ├── socks5.md
│   ├── exit-routing.md
│   ├── file-transfer.md
│   └── rpc.md
├── cli/                        # CLI reference
│   ├── overview.md
│   ├── run.md
│   ├── init.md
│   ├── setup.md
│   ├── cert.md
│   ├── rpc.md
│   ├── file-transfer.md
│   └── service.md
├── api/                        # HTTP API reference
│   ├── overview.md
│   ├── health.md
│   ├── metrics.md
│   ├── agents.md
│   ├── routes.md
│   ├── rpc.md
│   ├── file-transfer.md
│   └── dashboard.md
├── protocol/                   # Protocol documentation
│   ├── overview.md
│   ├── frames.md
│   ├── limits.md
│   └── routing.md
└── development/                # Development guides
    ├── building.md
    ├── testing.md
    └── docker.md
```

## Configuration

The site is configured to be deployed via static hosting.

Key configuration files:

- `docusaurus.config.ts` - Site configuration
- `sidebars.ts` - Sidebar navigation structure
- `src/css/custom.css` - Custom styling

## Deployment

The site can be deployed to:

- Static hosting (Netlify, Vercel, etc.)
- Static hosting
- Self-hosted web server

Build the static site and deploy the `build` directory:

```bash
npm run build
# Deploy contents of build/ directory
```

## Search

The site is configured to use Algolia DocSearch. Update the Algolia credentials in `docusaurus.config.ts`:

```typescript
algolia: {
  appId: 'YOUR_APP_ID',
  apiKey: 'YOUR_SEARCH_API_KEY',
  indexName: 'muti-metroo',
}
```

Or use the built-in search by installing the plugin:

```bash
npm install @docusaurus/theme-search-algolia
```

## License

Documentation is part of the Muti Metroo project. All rights reserved.
