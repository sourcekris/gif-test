package main

import (
	"flag"
	"fmt"
	"image/gif"
	"io/ioutil"
	"math"
	"os"
)

func main() {
	gifPath := flag.String("gif", "", "Path to GIF file to open.")

	flag.Parse()

	if len(*gifPath) == 0 {
		fmt.Println("You must provide a GIF.")
		os.Exit(1)
	}

	fh, err := os.Open(*gifPath)
	if err != nil {
		fmt.Printf("ReadFile: %s: %s\n", *gifPath, err)
		os.Exit(1)
	}

	defer func() {
		err := fh.Close()
		if err != nil {
			fmt.Printf("fh.Close: %s\n", err)
		}
	}()

	_, err = gif.DecodeAll(fh)
	if err != nil {
		fmt.Printf("gif.DecodeAll: %s\n", err)
	}

	err = decodeGIF(*gifPath)
	if err != nil {
		fmt.Printf("decodeGIF: %s\n", err)
		os.Exit(1)
	}
}

// GIF89a specification may be found at
// https://www.w3.org/Graphics/GIF/spec-gif89a.txt
func decodeGIF(path string) error {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("ReadFile: %s: %s", path, err)
	}

	fmt.Printf("gif is %d bytes\n", len(data))

	// Grammar is in Appendix B of GIF89a Specification

	// <GIF Data Stream> ::=     Header <Logical Screen> <Data>* Trailer

	idx := 0

	header, idx, err := readHeader(data, idx)
	if err != nil {
		return fmt.Errorf("readHeader: %s", err)
	}

	fmt.Printf("Header: %#v\n", header)

	screenDescriptor, idx, err := readLogicalScreenDescriptor(data, idx)
	if err != nil {
		return fmt.Errorf("reading logical screen descriptor: %s", err)
	}

	fmt.Printf("Logical Screen Descriptor: %#v\n", screenDescriptor)

	if screenDescriptor.HasGlobalColourTable {
		idx, err = readGlobalColourTable(screenDescriptor, data, idx)
		if err != nil {
			return fmt.Errorf("reading global colour table: %s", err)
		}
	}

	idx, err = readDataBlocksAndTrailer(data, idx)
	if err != nil {
		return fmt.Errorf("reading data blocks: %s", err)
	}

	// Must be pointing after the end of our data.
	if idx != len(data) {
		return fmt.Errorf("failed to parse entire image, at index %d, image size is %d",
			idx, len(data))
	}

	return nil
}

type header struct {
	Signature, Version string
}

func readHeader(data []byte, idx int) (*header, int, error) {
	// Header is 6 bytes.

	// First 3 bytes: "GIF"
	if data[idx] != 'G' || data[idx+1] != 'I' || data[idx+2] != 'F' {
		return nil, -1, fmt.Errorf("Header has unknown signature")
	}
	idx += 3

	// Next 3 bytes: Version. "87a" or "89a"

	if data[idx] == '8' && data[idx+1] == '7' && data[idx+2] == 'a' {
		return &header{Signature: "GIF", Version: "87a"}, idx + 3, nil
	}

	if data[idx] == '8' && data[idx+1] == '9' && data[idx+2] == 'a' {
		return &header{Signature: "GIF", Version: "89a"}, idx + 3, nil
	}

	return nil, -1, fmt.Errorf("Header has unknown version")
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

func readGlobalColourTable(screenDescriptor *logicalScreenDescriptor,
	data []byte, idx int) (int, error) {
	// Size is 3*2**(size of global colour table+1)
	sz := 3 * int(math.Exp2(float64(screenDescriptor.GlobalColourTableSize+1)))
	fmt.Printf("global colour table size: %d bytes\n", sz)

	idx += sz

	return idx, nil
}

// We read <Data>*
//
// When we see Trailer, we're done.
//
// <Data> ::=                <Graphic Block>  |
//                           <Special-Purpose Block>
func readDataBlocksAndTrailer(data []byte, idx int) (int, error) {
	// Read all blocks.
	imageCount := 0
	for {
		// Is it a Special-Purpose Block?
		//
		// I'm checking for this first since I added allowing Application Extension
		// in the Graphic Block.
		nextIdx, specialPurposeErr := readSpecialPurposeBlock(data, idx)
		if specialPurposeErr == nil {
			fmt.Printf("Read Special-Purpose Block\n")
			idx = nextIdx
			continue
		}

		// Is it a Graphic Block?
		nextIdx, graphicErr := readGraphicBlock(data, idx)
		if graphicErr == nil {
			fmt.Printf("Read Graphic Block %d\n", imageCount)
			imageCount++
			idx = nextIdx
			continue
		}

		// Is it a Trailer?
		nextIdx, trailerErr := readTrailer(data, idx)
		if trailerErr == nil {
			fmt.Printf("Read Trailer\n")
			return nextIdx, nil
		}

		if idx < len(data) && idx+1 < len(data) {
			fmt.Printf("next bytes: 0x%.2x 0x%.2x\n", data[idx], data[idx+1])
		} else if idx < len(data) {
			fmt.Printf("next byte: 0x%.2x\n", data[idx])
		}

		fmt.Printf("Not a Graphic Block because: %s\n", graphicErr)
		fmt.Printf("Not a Special-Purpose Block because: %s\n", specialPurposeErr)
		fmt.Printf("Not a Trailer because: %s\n", trailerErr)

		return -1, fmt.Errorf("unable to read data blocks. Could not parse block as Graphic Block, Special-Purpose Block, or Trailer")
	}
}

// <Graphic Block> ::= [Graphic Control Extension] <Graphic-Rendering Block>
func readGraphicBlock(data []byte, idx int) (int, error) {
	// Do we have an extension? It's optional.
	nextIdx, err := readGraphicControlExtension(data, idx)
	haveGraphicControlExtension := false
	if err == nil {
		fmt.Printf("Read Graphic Control Extension\n")
		idx = nextIdx
		haveGraphicControlExtension = true
	}

	// NOTE: Having an application extension here is invalid. However I have seen
	//   a gif in the wild with Graphic Control Extension, then this - the
	//   NETSCAPE extension. Even though the NETSCAPE extension spec says it must
	//   occur elsewhere (after screen descriptor I believe?)
	ext, nextIdx, err := readApplicationExtension(data, idx)
	if err == nil {
		fmt.Printf("Read Application Extension: %#v\n", ext)
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
	fmt.Printf("Read Graphic-Rendering Block\n")
	idx = nextIdx

	return idx, nil
}

func readGraphicControlExtension(data []byte, idx int) (int, error) {
	// We must have Extension Introducer. This says there is an Extension of some
	// kind.
	if data[idx] != 0x21 {
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
		fmt.Printf("Read Table-Based Image\n")
		return nextIdx, nil
	}

	nextIdx, err = readPlainTextExtension(data, idx)
	if err == nil {
		fmt.Printf("Read Plain Text Extension\n")
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
		fmt.Printf("Local Colour Table is present. Size: %d Actual size: %d\n",
			localColourTableSize, actualSize)
		idx += actualSize
		fmt.Printf("Read Local Colour Table\n")
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
	fmt.Printf("have %d bytes of image data\n", len(buf))

	// Do we have the End of Information code? Apparently it is not always
	// present. It is clear code+1. TODO I think the below is incorrect.

	clearCode := int(math.Exp2(float64(codeSize)))
	endOfInfoCode := clearCode + 1
	fmt.Printf("code size is %d, end of info code is %d\n", codeSize,
		endOfInfoCode)

	if codeSize != 8 {
		fmt.Printf("code size is not 8\n")
	} else {
		// XXX: This is definitely incorrect

		//if int(buf[len(buf)-1]) == endOfInfoCode {
		//	fmt.Printf("Found end of info code\n")
		//} else {
		//	fmt.Printf("No end of info code, last byte is %d\n", buf[len(buf)-1])
		//}

		//if int(buf[0]) == clearCode {
		//	fmt.Printf("Stream starts with clear code\n")
		//} else {
		//	fmt.Printf("Stream does not start with clear code\n")
		//}
	}

	return idx, nil
}

func readDataSubBlocks(data []byte, idx int) ([]byte, int, error) {
	buf := []byte{}

	for {
		sz := int(data[idx])
		idx++
		fmt.Printf("read data sub-block of size %d\n", sz)

		if sz == 0 {
			return buf, idx, nil
		}

		if sz == 1 {
			fmt.Printf("1 byte sub block is %#v\n", data[idx:idx+1])
		}

		buf = append(buf, data[idx:idx+sz]...)
		idx += sz
	}
}

func readPlainTextExtension(data []byte, idx int) (int, error) {
	// First byte is Extension Introducer. It says there is an extension.
	if data[idx] != 0x21 {
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
		fmt.Printf("Read Application Extension: %#v\n", ext)
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
	if data[idx] != 0x21 {
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
	if data[idx] != 0x3b {
		return -1, fmt.Errorf("not a trailer")
	}
	idx++

	return idx, nil
}
