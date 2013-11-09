CC = gcc

include platform_host.mk

ifneq ($(CROSS_PREFIX),)
	CC := $(CROSS_PREFIX)-$(CC)
	CXX := $(CROSS_PREFIX)-$(CXX)
else ifeq ($(HOST_OS), darwin)
	# clang on OS X
	CC = clang
	CXX = clang++
endif

include platform_target.mk

ifeq ($(TARGET_ARCH),x86)
	GOARCH = 386
else ifeq ($(TARGET_ARCH),x86_64)
	GOARCH = amd64
else ifeq ($(TARGET_ARCH),arm)
	GOARCH = arm
	GOARM = 6
endif

ifeq ($(TARGET_OS), windows)
	EXT = .exe
	GOOS = windows
else ifeq ($(TARGET_OS), darwin)
	EXT =
	GOOS = darwin
else ifeq ($(TARGET_OS), linux)
	EXT =
	GOOS = linux
else ifeq ($(TARGET_OS), android)
	EXT =
	GOOS = linux
endif

NAME = torrent2http
GO = go
GIT = git
GIT_VERSION = $(shell $(GIT) describe --always)
VERSION = $(patsubst v%,%,$(GIT_VERSION))
ZIP_FILE = $(ADDON_NAME)-$(VERSION).zip
CGO_ENABLED = 1
OUTPUT_NAME = $(NAME)$(EXT)
BUILD_PATH = build/$(TARGET_OS)_$(TARGET_ARCH)
LIBTORRENT_GO = github.com/steeve/libtorrent-go
LIBTORRENT_GO_HOME = $(GOPATH)/src/$(LIBTORRENT_GO)


all: clean dist

force:
	true

libtorrent-go: force
	cd $(LIBTORRENT_GO_HOME) && $(MAKE) $(MFLAGS)

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
	find $(LIBTORRENT_GO_HOME)/$(BUILD_PATH)/bin/ -type f -exec cp {} $(BUILD_PATH) \;

clean:
	cd $(LIBTORRENT_GO_HOME) && $(MAKE) $(MFLAGS) clean
	rm -rf $(BUILD_PATH)

distclean:
	rm -rf build
