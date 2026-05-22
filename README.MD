# go-bin2iso

This tool converts `bin/cue` images to `iso` images. Could come in handy for old, uhm, backup copies.

## Building

Build this utility using Golang. Automatically produces builds for some platforms are available in `Releases` section.

```sh
go build
```

### Cross Compiling

Cross compile this tool by setting `GOOS` and `GOARCH`, for example:

```sh
env GOOS=darwin GOARCH=amd64 go build
env GOOS=windows GOARCH=amd64 go build
env GOOS=linux GOARCH=amd64 go build
```

#### macOS Builds

The built artifacts are already (adhoc-)signed for macOS, however, after unpacking `bin2iso-macos-arm64.tar.gz` you might have to clear the quarantine flag of the executable using `xattr -c bin2iso-macos-arm64` shell command.

## Usage

Simply point the tool to the `bin` file you'd like to convert and it will produce an `iso` file.

```sh
./bin2iso game.bin
./bin2iso game.bin output.iso
```

