.PHONY: all pdf html clean install-deps mermaid combine

# Configuration
SRC_DIR := src
BUILD_DIR := build
PROCESSED_DIR := $(BUILD_DIR)/processed
ASSETS_DIR := assets
OUTPUT_PDF := $(BUILD_DIR)/muti-metroo-user-manual.pdf
OUTPUT_HTML := $(BUILD_DIR)/manual.html

# Version and build date (can be overridden via environment or command line)
VERSION ?= dev
BUILD_DATE ?= $(shell date +%Y-%m-%d)

# Find all source markdown files (sorted by filename)
SRC_FILES := $(sort $(wildcard $(SRC_DIR)/*.md))
PROCESSED_FILES := $(patsubst $(SRC_DIR)/%.md,$(PROCESSED_DIR)/%.md,$(SRC_FILES))

# Default target
all: pdf

# Install dependencies
install-deps:
	@echo "Installing Node.js dependencies..."
	npm install
	@echo ""
	@echo "Checking for pandoc..."
	@which pandoc > /dev/null || (echo "ERROR: pandoc not found. Install with: brew install pandoc (macOS) or apt install pandoc (Linux)" && exit 1)
	@echo "pandoc found: $$(pandoc --version | head -1)"
	@echo ""
	@echo "Checking for PDF engine (xelatex or tectonic)..."
	@(which xelatex > /dev/null || which tectonic > /dev/null) || (echo "ERROR: No PDF engine found. Install with: brew install tectonic (macOS) or apt install texlive-xetex (Linux)" && exit 1)
	@which xelatex > /dev/null && echo "xelatex found: $$(xelatex --version | head -1)" || true
	@which tectonic > /dev/null && echo "tectonic found: $$(tectonic --version)" || true
	@echo ""
	@echo "All dependencies installed."

# Process Mermaid diagrams in markdown files
# Output to PNG format for proper inline rendering (cropped to content)
$(PROCESSED_DIR)/%.md: $(SRC_DIR)/%.md
	@mkdir -p $(PROCESSED_DIR)
	@echo "Processing $<..."
	@if grep -q '```mermaid' "$<"; then \
		npx mmdc -i "$<" -o "$@" -e png -s 2 -p puppeteer-config.json 2>/dev/null || cp "$<" "$@"; \
	else \
		cp "$<" "$@"; \
	fi

mermaid: $(PROCESSED_FILES)

# Concatenate all processed markdown files with blank lines between them
$(BUILD_DIR)/combined.md: mermaid
	@echo "Combining markdown files..."
	@rm -f $@
	@for f in $(sort $(wildcard $(PROCESSED_DIR)/*.md)); do \
		cat "$$f" >> $@; \
		echo "" >> $@; \
		echo "" >> $@; \
	done
	@# Replace generic diagram captions with descriptive ones
	@sed -i.bak \
		-e 's/!\[diagram\](\.\/01-introduction-1/![Muti Metroo suite operator workflow](\.\/01-introduction-1/g' \
		-e 's/!\[diagram\](\.\/01-introduction-2/![Corporate network access topology](\.\/01-introduction-2/g' \
		-e 's/!\[diagram\](\.\/01-introduction-3/![Multi-site connectivity topology](\.\/01-introduction-3/g' \
		-e 's/!\[diagram\](\.\/07-agent-roles-1/![Agent roles in mesh network](\.\/07-agent-roles-1/g' \
		-e 's/!\[diagram\](\.\/07-agent-roles-2/![Bidirectional stream flow between agents](\.\/07-agent-roles-2/g' \
		-e 's/!\[diagram\](\.\/08-transports-1/![Mixed transport mesh topology](\.\/08-transports-1/g' \
		-e 's/!\[diagram\](\.\/10-tun-interface-1/![TUN interface traffic flow](\.\/10-tun-interface-1/g' \
		-e 's/!\[diagram\](\.\/10-tun-interface-2/![Multi-site network topology](\.\/10-tun-interface-2/g' \
		-e 's/!\[diagram\](\.\/16-port-forwarding-1/![SOCKS5 vs port forwarding traffic direction](\.\/16-port-forwarding-1/g' \
		$@ && rm -f $@.bak
	@# Copy diagram files to build dir so pandoc can find them
	@for ext in png pdf svg; do \
		if ls $(PROCESSED_DIR)/*.$$ext 1> /dev/null 2>&1; then \
			cp $(PROCESSED_DIR)/*.$$ext $(BUILD_DIR)/; \
		fi; \
	done
	@# Copy muti-metroo-manager screenshots from the docusaurus assets so the manual
	@# can reference them with relative ./ paths from the build dir.
	@if [ -d ../docs/static/img/screenshots/muti-metroo-manager ]; then \
		cp ../docs/static/img/screenshots/muti-metroo-manager/*.png $(BUILD_DIR)/ 2>/dev/null || true; \
	fi

combine: $(BUILD_DIR)/combined.md

# Build HTML preview (run from build dir so SVG paths resolve correctly)
html: $(BUILD_DIR)/combined.md
	@echo "Building HTML..."
	cd $(BUILD_DIR) && pandoc combined.md \
		--metadata-file=../metadata.yaml \
		--toc --toc-depth=3 \
		--standalone \
		-o manual.html
	@echo "HTML built: $(OUTPUT_HTML)"

# Detect PDF engine (prefer xelatex, fall back to tectonic)
PDF_ENGINE := $(shell which xelatex > /dev/null 2>&1 && echo xelatex || echo tectonic)

# Build PDF (run from build dir so SVG paths resolve correctly)
pdf: $(BUILD_DIR)/combined.md
	@echo "Building PDF with $(PDF_ENGINE)..."
	@echo "Version: $(VERSION), Build Date: $(BUILD_DATE)"
	@# Generate version definitions file
	@echo '\\renewcommand{\\docversion}{$(VERSION)}' > $(BUILD_DIR)/version.tex
	@echo '\\renewcommand{\\builddate}{$(BUILD_DATE)}' >> $(BUILD_DIR)/version.tex
	@# Copy logo to build dir
	@cp $(ASSETS_DIR)/logo.png $(BUILD_DIR)/
	cd $(BUILD_DIR) && pandoc combined.md \
		--metadata-file=../metadata.yaml \
		--pdf-engine=$(PDF_ENGINE) \
		--toc --toc-depth=3 \
		--include-in-header=../$(ASSETS_DIR)/header.tex \
		--include-in-header=version.tex \
		-V geometry:margin=1in \
		-V fontsize=11pt \
		-o muti-metroo-user-manual.pdf
	@echo "PDF built: $(OUTPUT_PDF)"

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	@echo "Build directory cleaned."

# Help
help:
	@echo "Muti Metroo User Manual - Build Targets"
	@echo ""
	@echo "  make pdf          - Build PDF (default)"
	@echo "  make html         - Build HTML preview"
	@echo "  make clean        - Remove build artifacts"
	@echo "  make install-deps - Check/install dependencies"
	@echo "  make help         - Show this help"
