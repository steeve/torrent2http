CC = gcc

include platform_host.mk

ifneq ($(CROSS_PREFIX),)
	CC := $(CROSS_HOME)/bin/$(CROSS_PREFIX)-$(CC)
	CXX := $(CROSS_HOME)/bin/$(CROSS_PREFIX)-$(CXX)
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
	GOARM = 7
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


all: clean libtorrent-go dist

force:
	true

libtorrent-go: force
	$(MAKE) -C $(LIBTORRENT_GO_HOME) clean all

run: build
ifeq ($(OS),Linux)
	LD_LIBRARY_PATH=$(BUILD_PATH):$$LD_LIBRARY_PATH $(BUILD_PATH)/$(OUTPUT_NAME)
endif
ifeq ($(OS),Darwin)
	DYLD_LIBRARY_PATH=$(BUILD_PATH):$$DYLD_LIBRARY_PATH $(BUILD_PATH)/$(OUTPUT_NAME)
endif


$(BUILD_PATH):
	mkdir -p $(BUILD_PATH)

$(BUILD_PATH)/$(OUTPUT_NAME): $(BUILD_PATH)
ifeq ($(TARGET_OS), windows)
	CC=$(CC) GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -v -o $(BUILD_PATH)/$(OUTPUT_NAME) -ldflags="-extld=$(CC)"
else
	CC=$(CC) GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -v -o $(BUILD_PATH)/$(OUTPUT_NAME) -ldflags="-linkmode=external -extld=$(CC)"
endif

vendor_libs_darwin:
	@for dylib in $(BUILD_PATH)/$(OUTPUT_NAME) $(BUILD_PATH)/libtorrent-rasterbar.7.dylib; do \
		for dep in `otool -L $$dylib | grep -v $$dylib | grep /usr/local | awk '{print $$1}'`; do \
			cp -f $$dep $(BUILD_PATH) && \
            chmod 644 $(BUILD_PATH)/`basename $$dep` && \
            install_name_tool -change $$dep @rpath/`basename $$dep` $(BUILD_PATH)/`basename $$dylib`; \
        done; \
    done

vendor_libs_android:

vendor_libs_linux:

vendor_libs_windows:
	cp -f $(GOPATH)/src/github.com/steeve/libtorrent-go/$(BUILD_PATH)/* $(BUILD_PATH)

dist: $(BUILD_PATH)/$(OUTPUT_NAME) vendor_libs_$(TARGET_OS)

clean:
	rm -rf $(BUILD_PATH)

distclean:
	rm -rf build


darwin: force
	$(MAKE) clean all

linux32: force
	$(MAKE) clean all TARGET_OS=linux CROSS_PREFIX=i586-pc-linux CROSS_HOME=/usr/local/gcc-4.8.1-for-linux32

linux64: force
	$(MAKE) clean all TARGET_OS=linux CROSS_PREFIX=x86_64-pc-linux CROSS_HOME=/usr/local/gcc-4.8.0-linux64

linux-rpi: force
	$(MAKE) clean all TARGET_OS=linux ARCH=arm CROSS_PREFIX=arm-linux-gnueabihf CROSS_HOME=/usr/local/gcc-linaro-arm-linux-gnueabihf-raspbian

android: force
	$(MAKE) clean all TARGET_OS=android ARCH=arm CROSS_PREFIX=arm-linux-androideabi CROSS_HOME=/usr/local/gcc-4.8.0-arm-linux-androideabi

windows: force
	$(MAKE) clean all TARGET_OS=windows CROSS_PREFIX=i586-mingw32 CROSS_HOME=/usr/local/gcc-4.8.0-mingw32
