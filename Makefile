NAME = torrent2http
CC = gcc
GO = go

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


CGO_ENABLED = 1
OUTPUT_NAME = $(NAME)$(EXT)
BUILD_PATH = build/$(OS)_$(ARCH)

all: package

re: clean all

libtorrent-go: force
	cd libtorrent-go && $(MAKE) $(MFLAGS)

run: build
ifeq ($(OS),Linux)
	LD_LIBRARY_PATH=$(BUILD_PATH):$$LD_LIBRARY_PATH $(BUILD_PATH)/$(OUTPUT_NAME)
endif
ifeq ($(OS),Darwin)
	DYLD_LIBRARY_PATH=$(BUILD_PATH):$$DYLD_LIBRARY_PATH $(BUILD_PATH)/$(OUTPUT_NAME)
endif

force:
	true

$(BUILD_PATH):
	mkdir -p $(BUILD_PATH)

build: $(BUILD_PATH) libtorrent-go force
	CC=$(CC) GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -v -o $(BUILD_PATH)/$(OUTPUT_NAME) -ldflags="-extld=$(CC)"

package: build
	find ./libtorrent-go/$(BUILD_PATH)/bin/ -type f -exec cp {} $(BUILD_PATH) \;

clean:
	cd libtorrent-go && $(MAKE) $(MFLAGS) clean
	rm -rf $(BUILD_PATH)

distclean:
	rm -rf build
