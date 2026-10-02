// virex-decode rebuilds a playable MP4 from a .virex file (pixel stream only).
//
//	virex-decode in.virex -o out.mp4
//	virex-decode in.virex -info
//	virex-decode in.virex -svir-json out.json
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"virex/codec/container"
	"virex/codec/decoder"
	"virex/svir/schema"
)

const usage = "usage: virex-decode in.virex -o out.mp4 | -info | -svir-json file"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("virex-decode", flag.ContinueOnError)
	out := fs.String("o", "", "output .mp4 file")
	info := fs.Bool("info", false, "print header and sections")
	svirJSON := fs.String("svir-json", "", "write the semantic data as JSON to this file ('-' for stdout)")
	noVerify := fs.Bool("no-verify", false, "skip CRC32 checks")

	// Allow the input file before or after the flags.
	var input string
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case !strings.HasPrefix(a, "-") && input == "":
			input = a
		case (a == "-o" || a == "-svir-json" || a == "--o" || a == "--svir-json") && i+1 < len(args):
			rest = append(rest, a, args[i+1])
			i++
		default:
			rest = append(rest, a)
		}
	}
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if input == "" || fs.NArg() > 0 || (*out == "" && !*info && *svirJSON == "") {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}

	f, err := os.Open(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rd, err := container.NewReader(f, st.Size())
	if err != nil {
		fmt.Fprintln(os.Stderr, "virex-decode:", err)
		return 1
	}

	if *info {
		printInfo(rd)
	}
	if *svirJSON != "" {
		if err := writeSVIR(rd, *svirJSON); err != nil {
			fmt.Fprintln(os.Stderr, "virex-decode:", err)
			return 1
		}
	}
	if *out != "" {
		o, err := os.Create(*out)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		di, err := decoder.Decode(rd, o, *noVerify)
		if cerr := o.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(*out)
			fmt.Fprintln(os.Stderr, "virex-decode:", err)
			return 1
		}
		checks := "ok"
		if !di.Verify {
			checks = "skipped"
		}
		fmt.Printf("%s -> %s (%d frames, checksums %s)\n", input, *out, di.Frames, checks)
	}
	return 0
}

func printInfo(rd *container.Reader) {
	h := rd.Header
	fmt.Printf("format %d.%d (needs reader >= %d.%d), schema %d.%d\n", h.FormatMajor, h.FormatMinor, h.MinReaderMajor, h.MinReaderMinor, h.SchemaMajor, h.SchemaMinor)
	fmt.Printf("video  %dx%d, nominal %d/%d fps, timescale %d, duration %.3fs, codec id %d\n",
		h.Width, h.Height, h.FPSNum, h.FPSDen, h.Timescale, float64(h.Duration)/float64(h.Timescale), h.PixelCodec)
	fmt.Printf("%-16s %-6s %12s %12s  %s\n", "section", "layer", "offset", "length", "crc32")
	for _, e := range rd.Sections {
		fmt.Printf("%-16s %-6d %12d %12d  %08x\n", e.Type, e.Layer, e.Offset, e.Length, e.CRC32)
	}
}

func writeSVIR(rd *container.Reader, path string) error {
	var docs []*schema.SVIRDocument
	for _, e := range rd.Sections {
		if e.Type != container.SectionSemantic {
			continue
		}
		b, err := rd.ReadSection(e)
		if err != nil {
			return err
		}
		d, err := schema.Unmarshal(b)
		if err != nil {
			return err
		}
		docs = append(docs, d)
	}
	if len(docs) == 0 {
		return fmt.Errorf("file has no SEMANTIC section")
	}
	m := docs[0]
	if len(docs) > 1 {
		m = schema.Merge(docs...)
	}
	j, err := schema.ToJSON(m)
	if err != nil {
		return err
	}
	if path == "-" {
		_, err = os.Stdout.Write(append(j, '\n'))
		return err
	}
	return os.WriteFile(path, append(j, '\n'), 0o644)
}
