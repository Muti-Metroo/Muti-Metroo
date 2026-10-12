# Building the Operator Slides

A short Marp slideshow distilling the operator manual for new operators.
Reads in five minutes, points at the operator manual for details and the
user manual for deep reference.

## Prerequisites

- Node.js (any reasonably recent version)
- Chromium / Chrome on the build host. Marp uses Puppeteer to render the
  PDF; on a fresh box you may need `npx marp --pdf --browser chrome ...`
  or Marp's bundled Chromium download.

## Build

```bash
cd operator-slides
npm install
make pdf            # build/muti-metroo-operator-slides.pdf
make html           # build/muti-metroo-operator-slides.html (preview)
make clean
```

## Editing

`slides.md` is a single Marp markdown file. Slides are separated by `---`.
Keep each slide short -- if you need more than five bullets, the topic
belongs in the operator manual instead.

## Release integration

The `operator-slides` job in `.github/workflows/release-please.yml` builds
`muti-metroo-operator-slides.pdf` and attaches it to each release alongside the
user and operator manual PDFs. The file lives next
to them in the release archive.
