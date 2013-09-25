NAME = torrent2http
GO = go

include Platform.inc

ifeq ($(OS),Windows_NT)
	EXT = .exe
endif

OUTPUT_NAME = $(NAME)$(EXT)
BUILD_PATH = build/$(OS)_$(ARCH)

all: libtorrent-go build

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

re: clean all

$(BUILD_PATH):
	mkdir -p $(BUILD_PATH)

build: $(BUILD_PATH) force
	$(GO) build -v -x -o $(BUILD_PATH)/$(OUTPUT_NAME)

package: build
	cp -f ./libtorrent-go/$(BUILD_PATH)/* $(BUILD_PATH)

clean:
	rm -rf $(BUILD_PATH)

distclean:
	rm -rf build
