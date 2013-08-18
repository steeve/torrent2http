NAME = torrent2http

LIBTORRENT_CFLAGS := $(shell pkg-config --cflags libtorrent-rasterbar)
LIBTORRENT_LIBS := $(shell pkg-config --libs libtorrent-rasterbar)
LIBTORRENT_GO_SONAME := github.com-steeve-libtorrent-go-libtorrent-swigcxx.so

HOMEBREW_DYLIB_PATH := /usr/local/lib
LIBTORRENT_DYLIB := libtorrent-rasterbar.7.dylib
LIBBOOST_DYLIB := libboost_system-mt.dylib

CFLAGS := $(LIBTORRENT_CFLAGS) -Wno-deprecated -Wno-deprecated-declarations
LDFLAGS := $(LIBTORRENT_LIBS)
SWIG_FEATURES := $(LIBTORRENT_CFLAGS) -I/usr/local/include

# Unfortunately I haven't found another way to give my flags to GCC when building
# through SWIG + Go.
CC := gcc $(CFLAGS) $(LDFLAGS)

OS := $(shell uname)

all: build fix_libs

build:
	CC="$(CC)" SWIG_FEATURES="$(SWIG_FEATURES)" go build -v -x $(NAME).go

fix_libs:
	# Copy the .so file here
	cp $(shell go env GOPATH)/src/github.com/steeve/libtorrent-go/$(LIBTORRENT_GO_SONAME) .
	# Make sure it uses local libtorrent
	install_name_tool -change $(HOMEBREW_DYLIB_PATH)/$(LIBTORRENT_DYLIB) @executable_path/$(LIBTORRENT_DYLIB) $(LIBTORRENT_GO_SONAME)

	# Make sure local libtorrent uses local boost
	cp $(HOMEBREW_DYLIB_PATH)/$(LIBBOOST_DYLIB) .
	cp $(HOMEBREW_DYLIB_PATH)/$(LIBTORRENT_DYLIB) .
	chmod +w $(LIBBOOST_DYLIB) $(LIBTORRENT_DYLIB)
	install_name_tool -change $(HOMEBREW_DYLIB_PATH)/$(LIBBOOST_DYLIB) @executable_path/$(LIBBOOST_DYLIB) $(LIBTORRENT_DYLIB)
