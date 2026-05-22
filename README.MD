# go-bin2iso

This tool convers `bin/cue` images to `iso` images. Could come in handy for old, uhm, backup copies.

## Building

Build this utility using Golang.

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

## Usage

Simply point the tool to the `bin` file you'd like to convert and it will produce an `iso` file.

```sh
./bin2iso game.bin
./bin2iso game.bin output.iso
```

