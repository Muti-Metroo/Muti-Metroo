.PHONY: all pdf html clean install-deps screenshots

SRC := slides.md
BUILD_DIR := build
OUTPUT_PDF := $(BUILD_DIR)/muti-metroo-operator-slides.pdf
OUTPUT_HTML := $(BUILD_DIR)/muti-metroo-operator-slides.html

# Source of truth for the dashboard screenshots is the Docusaurus assets
# directory. The Makefile mirrors them into assets/screenshots/ so Marp
# can pick them up without us duplicating images in the repo.
SCREENSHOT_SRC := ../docs/static/img/screenshots/muti-metroo-manager
SCREENSHOT_DST := assets/screenshots

# Marp CLI options. --allow-local-files lets the deck pull in image files
# from disk (logo, topology SVG, dashboard screenshots).
MARP_OPTS := --allow-local-files

all: pdf

install-deps:
	@echo "Installing Node.js dependencies..."
	npm install
	@echo ""
	@echo "Marp CLI installed."

$(BUILD_DIR):
	@mkdir -p $(BUILD_DIR)

# Sync dashboard screenshots from the docs tree.
screenshots:
	@mkdir -p $(SCREENSHOT_DST)
	@if [ -d $(SCREENSHOT_SRC) ]; then \
		cp $(SCREENSHOT_SRC)/*.png $(SCREENSHOT_DST)/ 2>/dev/null || true; \
		echo "Screenshots synced from $(SCREENSHOT_SRC)"; \
	else \
		echo "WARN: $(SCREENSHOT_SRC) not found; building without dashboard screenshots"; \
	fi

pdf: $(BUILD_DIR) screenshots
	@echo "Building slideshow PDF..."
	npx marp $(MARP_OPTS) --pdf $(SRC) -o $(OUTPUT_PDF)
	@echo "PDF built: $(OUTPUT_PDF)"

html: $(BUILD_DIR) screenshots
	@echo "Building slideshow HTML..."
	npx marp $(MARP_OPTS) --html $(SRC) -o $(OUTPUT_HTML)
	@echo "HTML built: $(OUTPUT_HTML)"

clean:
	rm -rf $(BUILD_DIR) $(SCREENSHOT_DST)
	@echo "Build directory and screenshot mirror cleaned."

help:
	@echo "Muti Metroo Operator Slides - Build Targets"
	@echo ""
	@echo "  make pdf          - Build slideshow PDF (default)"
	@echo "  make html         - Build standalone HTML"
	@echo "  make clean        - Remove build artifacts"
	@echo "  make install-deps - Install npm dependencies"
