# Building the Operator Manual

The operator manual uses the same toolchain as the user manual. If you can
build `user-manual/`, you can build this one. See `../user-manual/BUILD.md`
for the full prerequisite list (Pandoc, XeLaTeX or Tectonic, Node.js for
Mermaid).

## Quick build

```bash
cd operator-manual

# Install Node dependencies (first time only)
npm install

# Build PDF
make pdf

# Build HTML preview (for quick review)
make html

# Clean build artifacts
make clean

# Check all dependencies are installed
make install-deps
```

## Output

| File | Description |
|------|-------------|
| `build/muti-metroo-operator-manual.pdf` | Final PDF output |
| `build/manual.html` | HTML preview (for quick review) |
| `build/processed/` | Intermediate files (Mermaid-processed markdown) |
| `build/combined.md` | Combined markdown (all chapters) |

## Release integration

`release.sh` at the repo root builds the operator manual and attaches
`muti-metroo-operator-manual.pdf` to each release alongside `muti-metroo-user-manual.pdf`.
The release script also patches `__MUTI_METROO_RELEASE__`, `__MUTIAUK_RELEASE__`,
and `__METROO_MANAGER_RELEASE__` placeholders in `src/*.md` so download links resolve
to the matching tagged binaries.

## Editing the manual

This manual is a **practical guide**, not a feature reference. Keep new
chapters task-oriented:

- Lead with "the operator wants to do X"
- Show the command sequence and the expected output
- Note when to fall back to the user manual for reference detail

Do not duplicate content from the user manual. Cross-reference instead.

## Style

- Markdown source files in `src/`, numbered for ordering
  (`00-cover.md`, `01-introduction.md`, ...)
- ASCII only -- no emojis, no smart quotes
- Mermaid diagrams in fenced ` ```mermaid ` blocks; converted to PNG at
  build time
- Code blocks use language tags (`bash`, `yaml`, `json`)
- Heading depth max L3
