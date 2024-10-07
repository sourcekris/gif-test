package main

import (
	"flag"
	"fmt"
	"image/gif"
	"math"
	"os"
)

const (
	trailer   = 0x3b
	extension = 0x21
)

type ll int

const (
	debug ll = iota
	info
	warn
	errmsg
)

type cfg struct {
	logLevel  ll
	reprocess bool
	outFile   string
	write     bool
}

// patch records a desired change at a desired index.
type patch struct {
	index    int
	oldValue byte
	newValue byte
}

type gifstate struct {
	// validSignature is true when the GIF file signature is correct.
	validSignature bool

	// readTrailer is true when we've seen a trailer block.
	readTrailer bool

	// bytesOutstanding is a count of bytes not corresponding to data blocks before the trailer is encountered.
	bytesOutstanding int

	// patches is a slice of patches to apply when writing a fixed file.
	patches []*patch
}

var (
	conf = new(cfg)
)

// log emits a log message m depending on message level l and the global logLevel.
func log(l ll, m string) {
	if conf.logLevel <= l {
		fmt.Println(m)
	}
}

func main() {
	gifPath := flag.String("gif", "", "Path to GIF file to open.")
	alwaysReprocess := flag.Bool("reprocess", false, "Always re-interpret early trailers as Graphic Blocks.")
	outFile := flag.String("output", "", "File to write patched file to.")
	logLevel := flag.Int("l", 2, "Log level, default is 2, the most verbose is 0.")

	flag.Parse()

	conf.reprocess = *alwaysReprocess
	conf.logLevel = ll(*logLevel)

	if len(*gifPath) == 0 {
		log(errmsg, "You must provide a GIF file using the -gif flag.")
		os.Exit(1)
	}

	if len(*outFile) != 0 {
		conf.outFile = *outFile
		conf.write = true
	}

	fh, err := os.Open(*gifPath)
	if err != nil {
		log(errmsg, fmt.Sprintf("Open: %s: %s", *gifPath, err))
		os.Exit(1)
	}

	defer func() {
		if err := fh.Close(); err != nil {
			log(errmsg, fmt.Sprintf("fh.Close: %s", err))
		}
	}()

	if _, err = gif.DecodeAll(fh); err != nil {
		log(errmsg, fmt.Sprintf("gif.DecodeAll: %s", err))
	}

	gs := new(gifstate)

	if err := decodeGIF(*gifPath, gs); err != nil {
		log(errmsg, fmt.Sprintf("decodeGIF: %s", err))
		os.Exit(1)
	}
}

// GIF89a specification may be found at
// https://www.w3.org/Graphics/GIF/spec-gif89a.txt
func decodeGIF(path string, gs *gifstate) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("ReadFile: %s: %s", path, err)
	}

	log(debug, fmt.Sprintf("gif is %d bytes", len(data)))

	// Grammar is in Appendix B of GIF89a Specification

	// <GIF Data Stream> ::=     Header <Logical Screen> <Data>* Trailer

	idx := 0

	header, idx, err := readHeader(data, idx)
	if err != nil {
		return fmt.Errorf("readHeader: %s", err)
	}

	gs.validSignature = true

	log(info, fmt.Sprintf("Header: %s", header))

	screenDescriptor, idx, err := readLogicalScreenDescriptor(data, idx)
	if err != nil {
		return fmt.Errorf("reading logical screen descriptor: %s", err)
	}

	log(info, fmt.Sprintf("Logical Screen Descriptor: %#v", screenDescriptor))

	if screenDescriptor.HasGlobalColourTable {
		idx, err = readGlobalColourTable(screenDescriptor, idx)
		if err != nil {
			return fmt.Errorf("reading global colour table: %s", err)
		}
	}

	idx, err = readDataBlocksAndTrailer(data, idx, gs)
	if err != nil {
		return fmt.Errorf("reading data blocks: %s", err)
	}

	// Must be pointing after the end of our data.
	if idx != len(data) {
		return fmt.Errorf("failed to parse entire image, at index 0x%x, image size is %d",
			idx, len(data))
	}

	// Write a patched outfile.
	if conf.write && len(conf.outFile) > 0 {
		for _, p := range gs.patches {
			if data[p.index] == p.oldValue {
				data[p.index] = p.newValue
			} else {
				log(debug, fmt.Sprintf("patching requested at index %d, wanted %x got %x", p.index, p.oldValue, data[p.index]))
			}
		}

		err := os.WriteFile(conf.outFile, data, 0644)
		if err != nil {
			log(errmsg, fmt.Sprintf("failed writing output file: %v", err))
		}
	}

	return nil
}

type header struct {
	Signature, Version string
}

func (h *header) String() string {
	return fmt.Sprintf("Valid GIF header. Signature: %v Version: %v", h.Signature, h.Version)
}

func readHeader(data []byte, idx int) (*header, int, error) {
	// Header is 6 bytes.

	// First 3 bytes: "GIF"
	if data[idx] != 'G' || data[idx+1] != 'I' || data[idx+2] != 'F' {
		return nil, -1, fmt.Errorf("Header has unknown signature (missing GIF)")
	}
	idx += 3

	// Next 3 bytes: Version. "87a" or "89a"
	if data[idx] == '8' && data[idx+1] == '7' && data[idx+2] == 'a' {
		return &header{Signature: "GIF", Version: "87a"}, idx + 3, nil
	}

	if data[idx] == '8' && data[idx+1] == '9' && data[idx+2] == 'a' {
		return &header{Signature: "GIF", Version: "89a"}, idx + 3, nil
	}

	return nil, -1, fmt.Errorf("Header has unknown version. Not GIF87a or GIF89a. Check header bytes.")
}

type logicalScreenDescriptor struct {
	Width, Height           int
	HasGlobalColourTable    bool
	ColourResolution        int
	GlobalColourTableSorted bool
	GlobalColourTableSize   int
	BackgroundColourIndex   int
	PixelAspectRatio        int
}

func readLogicalScreenDescriptor(data []byte,
	idx int) (*logicalScreenDescriptor, int, error) {
	// This block is 7 bytes.

	screenDescriptor := &logicalScreenDescriptor{}

	// First 2 bytes: Logical screen width.
	idx += 2

	// Next 2 bytes: Logical screen height.
	idx += 2

	// Next byte: Packed fields.

	// Bit 0: Flag for whether there is a Global Colour Table
	screenDescriptor.HasGlobalColourTable = (data[idx] & 0x80) == 0x80

	// Bits 1, 2, 3: Colour resolution
	screenDescriptor.ColourResolution = int((data[idx]&0x70)>>4) + 1

	// Bit 4: Sort flag
	screenDescriptor.GlobalColourTableSorted = (data[idx] & 0x08) == 0x08

	// Bits 5, 6, 7: Size of global colour table.
	//screenDescriptor.GlobalColourTableSize = int(math.Exp2(float64(data[idx] & 0x07)))
	// Handle calculating the number of bytes when reading the table in.
	screenDescriptor.GlobalColourTableSize = int(data[idx] & 0x07)

	idx++

	// Next byte: Background Colour Index. Must be zero if there is no global
	// colour table.
	screenDescriptor.BackgroundColourIndex = int(data[idx])
	if !screenDescriptor.HasGlobalColourTable && data[idx] != 0 {
		return nil, -1, fmt.Errorf("No global colour table, but background colour index is set")
	}
	idx++

	// Next byte: Pixel aspect ratio
	screenDescriptor.PixelAspectRatio = int(data[idx])
	idx++

	return screenDescriptor, idx, nil
}

func readGlobalColourTable(screenDescriptor *logicalScreenDescriptor, idx int) (int, error) {
	// Size is 3*2**(size of global colour table+1)
	sz := 3 * int(math.Exp2(float64(screenDescriptor.GlobalColourTableSize+1)))
	log(debug, fmt.Sprintf("global colour table size: %d bytes", sz))

	idx += sz

	return idx, nil
}

// We read <Data>*
//
// When we see Trailer, we're done.
//
// <Data> ::=                <Graphic Block>  |
//
//	<Special-Purpose Block>
func readDataBlocksAndTrailer(data []byte, idx int, gs *gifstate) (int, error) {
	// Read all blocks.
	imageCount := 0
	for {
		// Is it a Special-Purpose Block?
		//
		// I'm checking for this first since I added allowing Application Extension
		// in the Graphic Block.
		nextIdx, specialPurposeErr := readSpecialPurposeBlock(data, idx)
		if specialPurposeErr == nil {
			log(info, "Read Special-Purpose Block")
			idx = nextIdx
			continue
		}

		// Is it a Graphic Block?
		nextIdx, graphicErr := readGraphicBlock(data, idx)
		if graphicErr == nil {
			log(info, fmt.Sprintf("Finished reading Graphic Block (#%d)", imageCount))
			imageCount++
			idx = nextIdx
			continue
		}

		// Is it a Trailer?
		nextIdx, trailerErr := readTrailer(data, idx)
		if trailerErr == nil {
			gs.readTrailer = true

			trailerMsg := fmt.Sprintf("Read trailer at index 0x%x of data size %d bytes", nextIdx, len(data))
			if nextIdx < len(data) {
				gs.bytesOutstanding = len(data) - nextIdx
				trailerMsg = fmt.Sprintf("%s: There is data (%d bytes) after the trailer indicating there may hidden frames or image data", trailerMsg, gs.bytesOutstanding)
			} else {
				gs.bytesOutstanding = 0
			}
			log(warn, trailerMsg)

			if gs.bytesOutstanding > 0 {
				if !conf.reprocess {
					log(warn, "Attempt to reprocess data after trailer block by passing -reprocess flag.")
					log(debug, fmt.Sprintf("Byte at idx(0x%x) is: 0x%x, Byte at nextIdx(0x%x) is: 0x%x", idx, data[idx], nextIdx, data[nextIdx]))
				}

				if conf.reprocess {
					// Make a copy of the image data changing the trailer byte to a Extension Block introducer.
					cd := make([]byte, len(data))
					copy(cd, data)
					cd[idx] = extension

					i, err := readGraphicBlock(cd, idx)
					if err == nil {
						log(warn, fmt.Sprintf("Successfully read an Extension Block from index 0x%x to 0x%x indicating that trailer byte (%x) at 0x%x was fake.", idx, i, trailer, idx))
						data[idx] = extension
						gs.patches = append(gs.patches, &patch{
							index:    idx,
							newValue: extension,
							oldValue: trailer,
						})
						continue
					}
				}
			}
			return nextIdx, nil
		}

		log(info, "Currently reading Data* and the next bytes are unrecognized. Must have one of Graphic Block, Special-Purpose Block, or a Trailer.")

		if idx < len(data) && idx+1 < len(data) {
			log(debug, fmt.Sprintf("next bytes: 0x%.2x 0x%.2x", data[idx], data[idx+1]))
		} else if idx < len(data) {
			log(debug, fmt.Sprintf("next byte: 0x%.2x", data[idx]))
		}

		log(info, fmt.Sprintf("Not a Graphic Block because: %s", graphicErr))
		log(info, fmt.Sprintf("Not a Special-Purpose Block because: %s", specialPurposeErr))
		log(info, fmt.Sprintf("Not a Trailer because: %s", trailerErr))

		return -1, fmt.Errorf("unable to read data blocks. Could not parse block as Graphic Block, Special-Purpose Block, or Trailer. at index in data 0x%x, total size of data is %d",
			idx, len(data))
	}
}

// <Graphic Block> ::= [Graphic Control Extension] <Graphic-Rendering Block>
func readGraphicBlock(data []byte, idx int) (int, error) {
	// Do we have an extension? It's optional.
	nextIdx, err := readGraphicControlExtension(data, idx)
	haveGraphicControlExtension := false
	if err == nil {
		log(info, "Read a Graphic Control Extension")
		idx = nextIdx
		haveGraphicControlExtension = true
	}

	// NOTE: Having an application extension here is invalid. However I have seen
	//   a gif in the wild with Graphic Control Extension, then this - the
	//   NETSCAPE extension. Even though the NETSCAPE extension spec says it must
	//   occur elsewhere (after screen descriptor I believe?)
	ext, nextIdx, err := readApplicationExtension(data, idx)
	if err == nil {
		log(info, fmt.Sprintf("Read Application Extension: %#v", ext))
		idx = nextIdx
		return idx, nil
	}

	nextIdx, err = readGraphicRenderingBlock(data, idx)
	if err != nil {
		if haveGraphicControlExtension {
			return -1, fmt.Errorf("read Graphic Control Extension, but could not read Graphic-Rendering Block: %s Next bytes: 0x%.2x 0x%.2x",
				err, data[idx], data[idx+1])
		}
		return -1, fmt.Errorf("unable to read graphic rendering block: %s", err)
	}
	log(info, "Read Graphic-Rendering Block")
	idx = nextIdx

	return idx, nil
}

func readGraphicControlExtension(data []byte, idx int) (int, error) {
	// We must have Extension Introducer. This says there is an Extension of some
	// kind.
	if data[idx] != extension {
		return -1, fmt.Errorf("no extension introducer")
	}
	idx++

	// Then we have Graphic Control Label. This identifies the Extension as a
	// Graphic Control Extension.
	if data[idx] != 0xf9 {
		return -1, fmt.Errorf("graphic control label is missing")
	}
	idx++

	// The next byte identifies the size of the block. The size does not include
	// the block terminator. For this extension it must be 4.
	sz := int(data[idx])
	if sz != 4 {
		return -1, fmt.Errorf("unexpected block size %d", sz)
	}
	idx++

	idx += sz

	// We should be at the block terminator.
	if data[idx] != 0 {
		return -1, fmt.Errorf("missing block terminator")
	}

	idx++

	return idx, nil
}

// <Graphic-Rendering Block> ::=  <Table-Based Image>  | Plain Text Extension
func readGraphicRenderingBlock(data []byte, idx int) (int, error) {
	nextIdx, err := readTableBasedImage(data, idx)
	if err == nil {
		log(info, "Finished reading a Table-Based Image")
		return nextIdx, nil
	}

	nextIdx, err = readPlainTextExtension(data, idx)
	if err == nil {
		log(info, "Read Plain Text Extension")
		return nextIdx, nil
	}

	return -1, fmt.Errorf("not a graphic-rendering block")
}

// <Table-Based Image> ::=   Image Descriptor [Local Color Table] Image Data
func readTableBasedImage(data []byte, idx int) (int, error) {
	// Image Descriptor

	// First byte is Image Separator. It must be 0x2c.
	if data[idx] != 0x2c {
		return -1, fmt.Errorf("no image separator")
	}
	idx++

	log(debug, "Found Table-Based Image, starting to read it...")

	// Next 2 bytes: Image Left Position
	idx += 2

	// Next 2 bytes: Image Top Position
	idx += 2

	// Next 2 bytes: Image Width
	idx += 2

	// Next 2 bytes: Image Height
	idx += 2

	// Next byte: Packed fields

	// Bit 0: Local Colour Table Flag. Whether there is a Local Colour Table.
	localColourTableFlag := (data[idx] & 0x80) == 0x80

	// Bit 1: Interlace flag

	// Bit 2: Sort flag

	// Bits 3 & 4: Reserved

	// Bits 5, 6, 7: Size of Local Colour Table
	localColourTableSize := int(data[idx] & 0x07)

	idx++

	// Local Colour Table. It is optional.
	if localColourTableFlag {
		actualSize := 3 * int(math.Exp2(float64(localColourTableSize+1)))
		log(debug, fmt.Sprintf("Local Colour Table is present. Size: %d Actual size: %d", localColourTableSize, actualSize))
		idx += actualSize
		log(debug, "Read Local Colour Table")
	}

	// Image Data.

	// First byte is LZW Minimum Code Size
	codeSize := int(data[idx])
	idx++

	// Then we have Image Data. It is a series of Data Sub-blocks.
	buf, idx, err := readDataSubBlocks(data, idx)
	if err != nil {
		return -1, fmt.Errorf("reading data sub blocks: %s", err)
	}
	log(debug, fmt.Sprintf("have %d bytes of image data", len(buf)))

	// Do we have the End of Information code? Apparently it is not always
	// present. It is clear code+1. TODO I think the below is incorrect.

	clearCode := int(math.Exp2(float64(codeSize)))
	endOfInfoCode := clearCode + 1
	log(debug, fmt.Sprintf("code size is %d, end of info code is %d", codeSize, endOfInfoCode))

	if codeSize != 8 {
		log(debug, "code size is not 8")
	}

	return idx, nil
}

func readDataSubBlocks(data []byte, idx int) ([]byte, int, error) {
	buf := []byte{}

	for {
		sz := int(data[idx])
		idx++
		log(debug, fmt.Sprintf("read data sub-block of size %d", sz))

		if sz == 0 {
			return buf, idx, nil
		}

		if sz == 1 {
			log(debug, fmt.Sprintf("1 byte sub block is %#v", data[idx:idx+1]))
		}

		buf = append(buf, data[idx:idx+sz]...)
		idx += sz
	}
}

func readPlainTextExtension(data []byte, idx int) (int, error) {
	// First byte is Extension Introducer. It says there is an extension.
	if data[idx] != extension {
		return -1, fmt.Errorf("no extension introducer")
	}
	idx++

	// Next byte is Plain Text Label.
	if data[idx] != 0x01 {
		return -1, fmt.Errorf("not a plain text extension")
	}
	idx++

	// Next is block size. Always 12.
	if data[idx] != 12 {
		return -1, fmt.Errorf("unexpected block size")
	}
	idx += 12

	// Then we have Plain Text Data. Data sub-blocks.
	_, idx, err := readDataSubBlocks(data, idx)
	if err != nil {
		return -1, fmt.Errorf("reading data sub blocks: %s", err)
	}

	return idx, nil
}

// <Special-Purpose Block> ::=    Application Extension  | Comment Extension
func readSpecialPurposeBlock(data []byte, idx int) (int, error) {
	ext, nextIdx, err := readApplicationExtension(data, idx)
	if err == nil {
		log(info, fmt.Sprintf("Read Application Extension: %#v", ext))
		return nextIdx, nil
	}

	//nextIdx, err := readCommentExtension(data, idx)

	return -1, fmt.Errorf("unable to read special-purpose block: %s", err)
}

type applicationExtension struct {
	Identifier         string
	AuthenticationCode string
}

func readApplicationExtension(data []byte,
	idx int) (*applicationExtension, int, error) {
	// First byte must be the Extension Introducer, saying this is an extension.
	// It must be 0x21
	if data[idx] != extension {
		return nil, -1, fmt.Errorf("no extension introducer")
	}
	idx++

	// Next byte is Extension Label. It identifies this as an Application
	// Extension.
	if data[idx] != 0xff {
		return nil, -1, fmt.Errorf("not an Application Extension")
	}
	idx++

	// Next byte identifies the size following this size field. It does not
	// include the Application Data. It must be 11.
	if int(data[idx]) != 11 {
		return nil, -1, fmt.Errorf("unexpected block size: %d", data[idx])
	}
	idx++

	ext := &applicationExtension{}

	// In the 11 bytes, the first 8 bytes are the application identifier. ASCII.
	ext.Identifier = string(data[idx : idx+8])
	idx += 8

	// Then next 3 bytes are the application authentication code.
	ext.AuthenticationCode = string(data[idx : idx+3])
	idx += 3

	// Then we have Application Data. This is a series of data sub-blocks.
	_, idx, err := readDataSubBlocks(data, idx)
	if err != nil {
		return nil, -1, fmt.Errorf("reading data sub blocks: %s", err)
	}

	return ext, idx, nil
}

func readTrailer(data []byte, idx int) (int, error) {
	if data[idx] != trailer {
		return -1, fmt.Errorf("not a trailer")
	}
	idx++

	return idx, nil
}
