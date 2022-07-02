GO ?= go
GOFLAGS ?=
DESTDIR ?=
PREFIX ?= /usr/local
BINDIR ?= bin
DATADIR ?= share

goldflags = \
	-X 'main.StaticDir=$(PREFIX)/$(DATADIR)/hottub/static' \
	-X 'main.TemplatesDir=$(PREFIX)/$(DATADIR)/hottub/templates'

all: hottub

hottub:
	$(GO) build $(GOFLAGS) -ldflags="$(goldflags)" .

install:
	mkdir -p $(DESTDIR)$(PREFIX)/$(BINDIR)
	mkdir -p $(DESTDIR)$(PREFIX)/$(DATADIR)/hottub
	cp -f hottub $(DESTDIR)$(PREFIX)/$(BINDIR)
	cp -rf static/ $(DESTDIR)$(PREFIX)/$(DATADIR)/hottub/static/
	cp -rf templates/ $(DESTDIR)$(PREFIX)/$(DATADIR)/hottub/templates/

clean:
	rm -f hottub

.PHONY: hottub
