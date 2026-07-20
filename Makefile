SHELL := /bin/zsh
PREFIX ?= $(HOME)/.local
BIN_DST := $(PREFIX)/bin/codex-run

.PHONY: help lint test ci install uninstall

help:
	@echo "targets:"
	@echo "  lint      zsh -n syntax check on bin/codex-run and tests"
	@echo "  test      run tests/run.zsh (hermetic; no ~/.codex touched)"
	@echo "  ci        lint + test"
	@echo "  install   symlink bin/codex-run into \$$PREFIX/bin (default ~/.local/bin)"
	@echo "  uninstall remove symlink"

lint:
	@zsh -n bin/codex-run
	@for f in tests/*.zsh; do zsh -n "$$f"; done
	@echo "lint: OK"

test:
	@zsh tests/run.zsh

ci: lint test

install:
	@mkdir -p "$(dir $(BIN_DST))"
	@ln -sfn "$(CURDIR)/bin/codex-run" "$(BIN_DST)"
	@echo "installed: $(BIN_DST) -> $(CURDIR)/bin/codex-run"

uninstall:
	@rm -f "$(BIN_DST)"
	@echo "removed: $(BIN_DST)"
