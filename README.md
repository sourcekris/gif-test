## GIFCheck

Tool for validating GIF files for certain types of issues. 

Currently supported tests:

- Signature validation
- Data past trailer check
 - Tool can also attempt to validate if data in a trailer block is valid image data.

 ## Examples

 Included is a
 [CTF challenge from BSides Canberra 2024 CTF by Cybears](https://gitlab.com/cybears/chals-2024/-/tree/main/misc/more-secrets)
 . This challenge  featured a valid GIF animation with a manipulated Graphic Block.
 The first byte of first block for frame 2 of the GIF animation was set to 0x3b
 which is parsed as a trailer. When rendered only frame 1 will render.

 This tool can read past this manipulation and decode the subsequent frames.

## TODO

Features todo will be tracked in Github issues but vaguely:

- Support writing a corrected GIF file to disk
- Support validation of other block types (other than just Graphics)
- Support other validations (to be researched)

## Attribution of original work and license

**Original Author:** https://github.com/horgh (William Storey)
**License:** GPL 3.0

This tool is derivative on the code from https://github.com/horgh/gif-test 
written by Github user https://github.com/horgh (William Storey). 

By using this tool, you agree to the terms of the GPL 3.0 license.