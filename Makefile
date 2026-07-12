.PHONY: build test install dist clean

BIN := bin/arkannie
PREFIX ?= $(HOME)/.local
VERSION := $(shell cat VERSION)
DISTNAME := arkannie-$(VERSION)
DISTDIR := dist/$(DISTNAME)

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/arkannie

test:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "gofmt: files need formatting" && exit 1)
	go vet ./...
	go test -race -cover ./...

install: build
	install -d $(PREFIX)/bin
	ln -sf $(abspath bin/arkannie.sh) $(PREFIX)/bin/arkannie

# dist packages a self-contained ARKANNIE_HOME tree (binary + shim + agents +
# identity + specs + manual) into dist/arkannie-<version>.tar.gz. Untarring it
# anywhere yields a working ARKANNIE_HOME; symlink bin/arkannie.sh onto PATH.
dist: build
	rm -rf $(DISTDIR) $(DISTDIR).tar.gz
	install -d $(DISTDIR)/bin
	cp $(BIN) $(DISTDIR)/bin/arkannie
	cp bin/arkannie.sh $(DISTDIR)/bin/arkannie.sh
	cp -r .agents $(DISTDIR)/.agents
	cp -r spec $(DISTDIR)/spec
	cp CLAUDE.md arkannie.md arkannie-absorb.md arkannie.config.yaml MANUAL.md VERSION $(DISTDIR)/
	tar -czf $(DISTDIR).tar.gz -C dist $(DISTNAME)
	@echo "packaged $(DISTDIR).tar.gz"

clean:
	rm -rf $(BIN) dist 2>/dev/null || true
