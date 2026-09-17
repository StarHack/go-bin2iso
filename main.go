package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type trackInfo struct {
	FileName   string
	Mode       string
	Sector     int
	IndexLBA   int64
	HasIndex   bool
	HasCue     bool
	TrackSeen  bool
}

var syncPattern = []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00}

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fatal("usage: bin2iso input.{bin,img,cue} [output.iso]")
	}

	inPath := os.Args[1]
	outPath := ""

	if len(os.Args) == 3 {
		outPath = os.Args[2]
	} else {
		ext := filepath.Ext(inPath)
		outPath = strings.TrimSuffix(inPath, ext) + ".iso"
	}

	if samePath(inPath, outPath) {
		fatal("input and output paths are the same")
	}

	imagePath, cuePath, err := resolveInput(inPath)
	if err != nil {
		fatal(err.Error())
	}

	info, err := detectLayout(imagePath, cuePath)
	if err != nil {
		fatal(err.Error())
	}

	if err := convert(imagePath, outPath, info); err != nil {
		_ = os.Remove(outPath)
		fatal(err.Error())
	}

	fmt.Printf("wrote %s\n", outPath)
}

func fatal(s string) {
	fmt.Fprintln(os.Stderr, "error:", s)
	os.Exit(1)
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}

	return aa == bb
}

func resolveInput(inputPath string) (imagePath, cuePath string, err error) {
	switch strings.ToLower(filepath.Ext(inputPath)) {
	case ".cue":
		return resolveFromCue(inputPath)
	case ".bin", ".img":
		return inputPath, findCue(inputPath), nil
	default:
		return "", "", fmt.Errorf("unsupported input file type %q; use .bin, .img, or .cue", filepath.Ext(inputPath))
	}
}

func resolveFromCue(inputPath string) (string, string, error) {
	dir := filepath.Dir(inputPath)
	cueBase := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))

	ti, err := parseCue(inputPath, "")
	if err != nil {
		return "", inputPath, err
	}

	imagePath := findImageFile(dir, cueBase, ti.FileName)
	if imagePath == "" {
		ref := ti.FileName
		if ref == "" {
			ref = cueBase + ".bin/.img"
		}

		return "", inputPath, fmt.Errorf("CUE references %q but no matching .bin or .img file was found", ref)
	}

	return imagePath, inputPath, nil
}

func normalizeCueFileName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "\"")
	name = strings.ReplaceAll(name, "\\", "/")

	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}

	return name
}

func baseNameWithoutExt(name string) string {
	name = normalizeCueFileName(name)
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext)
}

func findImageFile(dir, cueBase, referenced string) string {
	referenced = normalizeCueFileName(referenced)

	if referenced != "" {
		if p := findNamedFile(dir, referenced); p != "" && isImageExt(p) {
			return p
		}
	}

	if p := findFileByBase(dir, cueBase, ".bin", ".img"); p != "" {
		return p
	}

	if referenced != "" {
		refBase := baseNameWithoutExt(referenced)
		if !strings.EqualFold(refBase, cueBase) {
			if p := findFileByBase(dir, refBase, ".bin", ".img"); p != "" {
				return p
			}
		}
	}

	return ""
}

func findFileByBase(dir, base string, extensions ...string) string {
	base = normalizeCueFileName(base)

	for _, ext := range extensions {
		candidates := []string{
			filepath.Join(dir, base+ext),
			filepath.Join(dir, base+strings.ToUpper(ext)),
		}

		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		ext := filepath.Ext(name)

		for _, wantExt := range extensions {
			if strings.EqualFold(ext, wantExt) &&
				strings.EqualFold(strings.TrimSuffix(name, ext), base) {
				return filepath.Join(dir, name)
			}
		}
	}

	return ""
}

func findNamedFile(dir, name string) string {
	name = normalizeCueFileName(name)
	if name == "" {
		return ""
	}

	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); err == nil {
		return p
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		if strings.EqualFold(e.Name(), name) {
			return filepath.Join(dir, e.Name())
		}
	}

	return ""
}

func isImageExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".bin" || ext == ".img"
}

func sameBaseName(a, b string) bool {
	aBase := baseNameWithoutExt(a)
	bBase := baseNameWithoutExt(b)

	return strings.EqualFold(aBase, bBase)
}

func detectLayout(imagePath, cuePath string) (trackInfo, error) {
	if cuePath != "" {
		ti, err := parseCue(cuePath, imagePath)
		if err != nil {
			return trackInfo{}, err
		}

		ti.HasCue = true
		return ti, nil
	}

	f, err := os.Open(imagePath)
	if err != nil {
		return trackInfo{}, err
	}

	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return trackInfo{}, err
	}

	buf := make([]byte, 2352)
	n, _ := io.ReadFull(f, buf)
	size := st.Size()

	if n >= 2352 && bytes.Equal(buf[:12], syncPattern) {
		mode := buf[15]

		if mode == 1 {
			return trackInfo{
				Mode:   "MODE1/2352",
				Sector: 2352,
			}, nil
		}

		if mode == 2 {
			return trackInfo{
				Mode:   "MODE2/2352",
				Sector: 2352,
			}, nil
		}
	}

	if size%2048 == 0 {
		return trackInfo{
			Mode:   "MODE1/2048",
			Sector: 2048,
		}, nil
	}

	if size%2352 == 0 {
		return trackInfo{}, errors.New("2352-byte sectors detected, but sector sync/header was not recognized and no matching CUE file was found")
	}

	return trackInfo{}, errors.New("could not detect disc image layout; provide a matching CUE file or a 2048-byte-sector image")
}

func findCue(imagePath string) string {
	dir := filepath.Dir(imagePath)
	base := strings.TrimSuffix(filepath.Base(imagePath), filepath.Ext(imagePath))

	return findFileByBase(dir, base, ".cue")
}

func parseCue(cuePath, imagePath string) (trackInfo, error) {
	f, err := os.Open(cuePath)
	if err != nil {
		return trackInfo{}, err
	}

	defer f.Close()

	var currentFile string
	var chosen *trackInfo
	var current *trackInfo

	fileRe := regexp.MustCompile(`(?i)^\s*FILE\s+(?:"([^"]+)"|(\S+))\s+\S+`)
	trackRe := regexp.MustCompile(`(?i)^\s*TRACK\s+(\d+)\s+(\S+)`)
	indexRe := regexp.MustCompile(`(?i)^\s*INDEX\s+01\s+(\d+):(\d+):(\d+)`)

	sc := bufio.NewScanner(f)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		if m := fileRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			if name == "" {
				name = m[2]
			}

			currentFile = normalizeCueFileName(name)
			current = nil
			continue
		}

		if m := trackRe.FindStringSubmatch(line); m != nil {
			mode := strings.ToUpper(m[2])
			sector, ok := sectorSizeForMode(mode)

			current = &trackInfo{
				FileName:  currentFile,
				Mode:      mode,
				Sector:    sector,
				TrackSeen: true,
			}

			if !ok {
				current = nil
				continue
			}

			if isDataMode(mode) && chosen == nil && trackMatchesImage(currentFile, imagePath) {
				cp := *current
				chosen = &cp
			}

			continue
		}

		if m := indexRe.FindStringSubmatch(line); m != nil && current != nil {
			lba, err := parseMSF(m[1], m[2], m[3])
			if err != nil {
				return trackInfo{}, err
			}

			current.IndexLBA = lba
			current.HasIndex = true

			if chosen != nil &&
				strings.EqualFold(chosen.Mode, current.Mode) &&
				sameBaseName(chosen.FileName, current.FileName) {
				chosen.IndexLBA = lba
				chosen.HasIndex = true
			}
		}
	}

	if err := sc.Err(); err != nil {
		return trackInfo{}, err
	}

	if chosen == nil {
		if imagePath == "" {
			return trackInfo{}, errors.New("CUE file found, but no supported data track was found")
		}

		return trackInfo{}, errors.New("matching CUE file found, but no supported data track for this image was found")
	}

	return *chosen, nil
}

func trackMatchesImage(cueFile, imagePath string) bool {
	if imagePath == "" {
		return true
	}

	if cueFile == "" {
		return true
	}

	return sameBaseName(cueFile, imagePath)
}

func sectorSizeForMode(mode string) (int, bool) {
	switch strings.ToUpper(mode) {
	case "MODE1/2048":
		return 2048, true
	case "MODE1/2352":
		return 2352, true
	case "MODE2/2352":
		return 2352, true
	default:
		return 0, false
	}
}

func isDataMode(mode string) bool {
	mode = strings.ToUpper(mode)

	return mode == "MODE1/2048" ||
		mode == "MODE1/2352" ||
		mode == "MODE2/2352"
}

func parseMSF(mm, ss, ff string) (int64, error) {
	m, err := strconv.ParseInt(mm, 10, 64)
	if err != nil {
		return 0, err
	}

	s, err := strconv.ParseInt(ss, 10, 64)
	if err != nil {
		return 0, err
	}

	f, err := strconv.ParseInt(ff, 10, 64)
	if err != nil {
		return 0, err
	}

	if s < 0 || s > 59 || f < 0 || f > 74 {
		return 0, errors.New("invalid CUE MSF timestamp")
	}

	return m*60*75 + s*75 + f, nil
}

func convert(inPath, outPath string, ti trackInfo) error {
	in, err := os.Open(inPath)
	if err != nil {
		return err
	}

	defer in.Close()

	st, err := in.Stat()
	if err != nil {
		return err
	}

	start := ti.IndexLBA * int64(ti.Sector)
	if start < 0 || start > st.Size() {
		return errors.New("CUE index points outside image file")
	}

	if _, err := in.Seek(start, io.SeekStart); err != nil {
		return err
	}

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}

	defer out.Close()

	switch strings.ToUpper(ti.Mode) {
	case "MODE1/2048":
		return copyAligned(in, out, st.Size()-start, 2048)
	case "MODE1/2352":
		return extract2352(in, out, st.Size()-start, 1)
	case "MODE2/2352":
		return extract2352(in, out, st.Size()-start, 2)
	default:
		return fmt.Errorf("unsupported track mode: %s", ti.Mode)
	}
}

func copyAligned(in *os.File, out *os.File, remaining int64, sector int) error {
	if remaining%int64(sector) != 0 {
		return fmt.Errorf("input size after CUE index is not aligned to %d-byte sectors", sector)
	}

	_, err := io.Copy(out, in)
	return err
}

func extract2352(in *os.File, out *os.File, remaining int64, expectedMode byte) error {
	if remaining%2352 != 0 {
		return errors.New("input size after CUE index is not aligned to 2352-byte sectors")
	}

	buf := make([]byte, 2352)
	sectorNum := int64(0)

	for {
		_, err := io.ReadFull(in, buf)

		if err == io.EOF {
			return nil
		}

		if err == io.ErrUnexpectedEOF {
			return errors.New("truncated final sector")
		}

		if err != nil {
			return err
		}

		if !bytes.Equal(buf[:12], syncPattern) {
			return fmt.Errorf("sector %d has invalid sync pattern", sectorNum)
		}

		mode := buf[15]
		if mode != expectedMode {
			return fmt.Errorf("sector %d has mode %d, expected mode %d", sectorNum, mode, expectedMode)
		}

		if mode == 1 {
			if _, err := out.Write(buf[16 : 16+2048]); err != nil {
				return err
			}
		} else if mode == 2 {
			submode := buf[18]

			if submode&0x20 != 0 {
				return fmt.Errorf("sector %d is Mode 2 Form 2; cannot convert cleanly to 2048-byte ISO sectors", sectorNum)
			}

			if _, err := out.Write(buf[24 : 24+2048]); err != nil {
				return err
			}
		}

		sectorNum++
	}
}
