CC = gcc

include OS.inc
ifeq ($(OS),Darwin)
	# clang on OS X
	CC = clang
	CXX = clang++
endif
ifneq ($(CROSS_PREFIX),)
	CC := $(CROSS_PREFIX)-$(CC)
	CXX := $(CROSS_PREFIX)-$(CXX)
endif
include Arch.inc

ifeq ($(ARCH),x86)
	GOARCH = 386
endif
ifeq ($(ARCH),x86_64)
	GOARCH = amd64
endif
ifeq ($(ARCH),arm)
	GOARCH = arm
	GOARM = 6
endif

ifeq ($(OS),Windows_NT)
	EXT = .exe
	GOOS = windows
endif
ifeq ($(OS),Darwin)
	EXT =
	GOOS = darwin
endif
ifeq ($(OS),Linux)
	EXT =
	GOOS = linux
endif

NAME = torrent2http
GO = go
GIT = git
ADDON_NAME = script.module.$(NAME)
GIT_VERSION = $(shell $(GIT) describe --always)
VERSION = $(patsubst v%,%,$(GIT_VERSION))
ZIP_FILE = $(ADDON_NAME)-$(VERSION).zip
CGO_ENABLED = 1
OUTPUT_NAME = $(NAME)$(EXT)
BUILD_PATH = build/$(OS)_$(ARCH)
ARCH_ZIP_FILE = $(ADDON_NAME)-$(VERSION).$(OS)_$(ARCH).zip


all: clean dist

force:
	true

libtorrent-go: force
	cd libtorrent-go && $(MAKE) $(MFLAGS)

run: build
ifeq ($(OS),Linux)
	LD_LIBRARY_PATH=$(BUILD_PATH):$$LD_LIBRARY_PATH $(BUILD_PATH)/$(OUTPUT_NAME)
endif
ifeq ($(OS),Darwin)
	DYLD_LIBRARY_PATH=$(BUILD_PATH):$$DYLD_LIBRARY_PATH $(BUILD_PATH)/$(OUTPUT_NAME)
endif


$(BUILD_PATH):
	mkdir -p $(BUILD_PATH)

$(BUILD_PATH)/$(OUTPUT_NAME): $(BUILD_PATH) libtorrent-go force
	CC=$(CC) GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -v -o $(BUILD_PATH)/$(OUTPUT_NAME) -ldflags="-extld=$(CC)"

dist: $(BUILD_PATH)/$(OUTPUT_NAME)
	find ./libtorrent-go/$(BUILD_PATH)/bin/ -type f -exec cp {} $(BUILD_PATH) \;

clean:
	cd libtorrent-go && $(MAKE) $(MFLAGS) clean
	rm -rf $(BUILD_PATH)

distclean:
	rm -rf build


addon.xml:
	sed s/\$$VERSION/$(VERSION)/g < addon.xml.tpl > $@

$(ADDON_NAME): addon.xml
	mkdir -p $(ADDON_NAME)/bin
	ln -s `pwd`/build/* $(ADDON_NAME)/bin
	ln -s `pwd`/addon.xml $(ADDON_NAME)

$(ARCH_ZIP_FILE): $(ADDON_NAME)
	zip -9 -r $@ $</addon.xml $</bin/$(OS)_$(ARCH)
	rm -rf $(ADDON_NAME)

$(ZIP_FILE): $(ADDON_NAME)
	zip -9 -r $@ $<
	rm -rf $(ADDON_NAME)

arch_zip: $(ARCH_ZIP_FILE)

zip: $(ZIP_FILE)
